package parallel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rileyhilliard/rr/internal/config"
)

// Orchestrator coordinates parallel task execution across multiple hosts.
type Orchestrator struct {
	tasks    []TaskInfo
	hosts    map[string]config.Host
	hostList []string // Ordered list of host names
	config   Config
	resolved *config.ResolvedConfig

	// Sync tracking
	syncedHosts map[string]bool
	syncMu      sync.Mutex

	// Performance tracking for work-stealing optimization
	// Tracks first-task completion time per host to identify slow hosts
	hostFirstTaskTime map[string]time.Duration
	hostTimeMu        sync.Mutex
	fastestFirstTask  time.Duration // Duration of fastest first-task completion

	// Setup tracking (runs once per host after sync)
	// setupHosts tracks whether setup has been attempted for each host
	// setupErrors stores any error from setup failure (nil = success)
	setupHosts  map[string]bool
	setupErrors map[string]error
	setupMu     sync.Mutex

	// Unavailable host tracking for graceful task re-queuing
	// When a host's SSH connection fails, it's marked unavailable and
	// any tasks assigned to it are returned to the queue for other hosts
	unavailableHosts map[string]bool
	unavailableMu    sync.Mutex

	// workerHosts is the subset of hostList that got a worker goroutine in
	// this run. Tasks with host restrictions can only be served by a worker
	// host, so this is what hasAvailableHostFor consults.
	workerHosts []string

	// Output management
	outputMgr *OutputManager

	// Results collection
	results   []TaskResult
	resultsMu sync.Mutex

	// Cancellation
	cancelOnce sync.Once
	cancelFunc context.CancelFunc
}

// NewOrchestrator creates a new orchestrator for parallel task execution.
// The hostOrder parameter specifies the priority order for host selection - hosts earlier
// in the list are preferred. If hostOrder is nil, an arbitrary order is used.
func NewOrchestrator(tasks []TaskInfo, hosts map[string]config.Host, hostOrder []string, resolved *config.ResolvedConfig, cfg Config) *Orchestrator {
	// Use provided order if available, otherwise fall back to map iteration (arbitrary order)
	hostList := hostOrder
	if len(hostList) == 0 && len(hosts) > 0 {
		hostList = make([]string, 0, len(hosts))
		for name := range hosts {
			hostList = append(hostList, name)
		}
	}

	return &Orchestrator{
		tasks:             tasks,
		hosts:             hosts,
		hostList:          hostList,
		config:            cfg,
		resolved:          resolved,
		syncedHosts:       make(map[string]bool),
		hostFirstTaskTime: make(map[string]time.Duration),
		setupHosts:        make(map[string]bool),
		setupErrors:       make(map[string]error),
		unavailableHosts:  make(map[string]bool),
		results:           make([]TaskResult, 0, len(tasks)),
	}
}

// Run executes all tasks in parallel across available hosts.
//
// Work-stealing queue design: Tasks are placed in a buffered channel that acts
// as a shared queue. Each host worker pulls tasks from this channel independently
// (work stealing). This approach:
//   - Naturally load-balances: fast hosts grab more work
//   - Handles heterogeneous hosts: no pre-assignment needed
//   - Simplifies cancellation: just close the channel
//
// Graceful host failover: When a host's SSH connection fails, the task is
// returned to the queue for other hosts to pick up. The unavailable host is
// marked so no more tasks are assigned to it. Only if ALL hosts become
// unavailable does execution fail.
//
// The channel-based approach avoids explicit locking on the queue itself since
// Go channels are already synchronized.
//
// If no remote hosts are configured, tasks run locally (sequentially).
func (o *Orchestrator) Run(ctx context.Context) (*Result, error) {
	if len(o.tasks) == 0 {
		return &Result{}, nil
	}

	// If no hosts configured, run locally
	if len(o.hosts) == 0 {
		return o.runLocal(ctx)
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	o.cancelFunc = cancel
	defer cancel()

	// Determine TTY status for output manager
	isTTY := isTerminal()

	// Initialize output manager
	o.outputMgr = NewOutputManager(o.config.OutputMode, isTTY)
	defer o.outputMgr.Close()

	// Show all tasks as pending upfront (pass full TaskInfo for Index tracking)
	o.outputMgr.InitTasks(o.tasks)

	startTime := time.Now()

	// Create task queue (channel-based work stealing).
	// We use an unbuffered send with a goroutine dispatcher to allow re-queuing.
	taskQueue := make(chan TaskInfo, len(o.tasks))

	// Re-queue channel for tasks that need to be assigned to a different host
	// (e.g., when the original host is unavailable)
	requeueChan := make(chan TaskInfo, len(o.tasks))

	// Determine number of workers, then pick which hosts get one. A task
	// pinned to a host (`hosts:` on the subtask) needs a worker on that
	// host, so the pick can exceed the count when the first N hosts in
	// priority order would leave a pinned task with nowhere to run.
	numWorkers := len(o.hostList)
	if o.config.MaxParallel > 0 && o.config.MaxParallel < numWorkers {
		numWorkers = o.config.MaxParallel
	}
	if numWorkers > len(o.tasks) {
		numWorkers = len(o.tasks)
	}
	o.workerHosts = o.pickWorkerHosts(numWorkers)
	numWorkers = len(o.workerHosts)

	// Result channel for collecting task results
	resultChan := make(chan TaskResult, len(o.tasks))

	// Signal channel closed when all expected results have been collected.
	// This breaks the circular dependency between the dispatcher (waiting on
	// requeueChan) and workers (waiting on taskQueue) by letting the dispatcher
	// know it can exit and close taskQueue, which unblocks the workers.
	allDone := make(chan struct{})

	// Failed flag for fail-fast mode
	var failed bool
	var failedMu sync.Mutex

	// Task dispatcher goroutine: feeds tasks to workers, handles re-queuing
	dispatcherDone := make(chan struct{})
	go func() {
		defer close(dispatcherDone)
		defer close(taskQueue)

		// When the dispatcher exits with all hosts unavailable, tasks already
		// sent to taskQueue may have no worker to consume them. Drain any
		// unconsumed tasks and produce failure results so every task is
		// accounted for in the final result set.
		defer o.drainUnconsumedTasks(taskQueue, resultChan)

		// Initial tasks
		pending := make([]TaskInfo, len(o.tasks))
		copy(pending, o.tasks)

		for len(pending) > 0 {
			// Check if all hosts are unavailable
			if o.allHostsUnavailable() {
				// Drain any requeued tasks back into pending before failing them
			drainInitial:
				for {
					select {
					case task := <-requeueChan:
						pending = append(pending, task)
					default:
						break drainInitial
					}
				}
				// All hosts down - mark remaining tasks as failed
				for _, task := range pending {
					result := TaskResult{
						TaskName:  task.Name,
						TaskIndex: task.Index,
						Command:   task.Command,
						Host:      "none",
						ExitCode:  1,
						Error:     fmt.Errorf("all hosts unavailable"),
						StartTime: time.Now(),
						EndTime:   time.Now(),
					}
					resultChan <- result
				}
				return
			}

			select {
			case <-ctx.Done():
				return
			case <-allDone:
				return
			case task := <-requeueChan:
				// Task was re-queued: its host went unavailable, or a worker
				// on a host the task is not allowed on bounced it.
				if !o.hasAvailableHostFor(task) {
					resultChan <- noHostResult(task)
					continue
				}
				pending = append(pending, task)
			case taskQueue <- pending[0]:
				// Task dispatched to a worker
				pending = pending[1:]
			}
		}

		// All initial tasks dispatched. Wait for either requeued tasks or completion.
		// We listen on allDone to break the circular dependency: without it, the
		// dispatcher blocks on requeueChan (closed only after workers exit), while
		// workers block on taskQueue (closed only when dispatcher exits).
		for {
			select {
			case <-ctx.Done():
				return
			case <-allDone:
				return
			case task, ok := <-requeueChan:
				if !ok {
					return
				}

				// No live host may run this task (every host down, or the
				// task's allowed hosts are all down): fail it now instead
				// of bouncing it between workers forever.
				if !o.hasAvailableHostFor(task) {
					resultChan <- noHostResult(task)
					continue
				}

				// Dispatch re-queued task to an available worker
				select {
				case <-ctx.Done():
					return
				case <-allDone:
					return
				case taskQueue <- task:
					// Re-queued task dispatched
				}
			}
		}
	}()

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		hostName := o.workerHosts[i]
		wg.Add(1)
		go func(hostName string) {
			defer wg.Done()
			o.hostWorkerWithRequeue(ctx, hostName, taskQueue, requeueChan, resultChan, &failed, &failedMu)
		}(hostName)
	}

	// Collect results, signal completion, then wait for shutdown
	go func() {
		wg.Wait()
		close(requeueChan)
		<-dispatcherDone
		close(resultChan)
	}()

	// Gather results
	expectedResults := len(o.tasks)
	collected := 0
	var closeAllDone sync.Once
	hostsUsed := make(map[string]bool)
	for result := range resultChan {
		o.resultsMu.Lock()
		o.results = append(o.results, result)
		o.resultsMu.Unlock()
		if result.Host != "none" {
			hostsUsed[result.Host] = true
		}
		collected++
		if collected >= expectedResults {
			closeAllDone.Do(func() { close(allDone) })
		}
	}

	// Build final result
	duration := time.Since(startTime)
	return o.buildResult(duration, hostsUsed), nil
}

// hostWorkerWithRequeue is a goroutine that grabs tasks from the queue and executes them.
// If the host becomes unavailable (SSH connection fails), tasks are re-queued for other hosts.
func (o *Orchestrator) hostWorkerWithRequeue(
	ctx context.Context,
	hostName string,
	taskQueue <-chan TaskInfo,
	requeueChan chan<- TaskInfo,
	resultChan chan<- TaskResult,
	failed *bool,
	failedMu *sync.Mutex,
) {
	worker := &hostWorker{
		orchestrator: o,
		hostName:     hostName,
		host:         o.hosts[hostName],
		resultChan:   resultChan,
		failed:       failed,
		failedMu:     failedMu,
	}
	defer worker.Close()

	isFirstTask := true

	for {
		// Check if this host is marked unavailable
		if o.isHostUnavailable(hostName) {
			return
		}

		// Check for cancellation
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Check fail-fast
		if o.config.FailFast {
			failedMu.Lock()
			shouldStop := *failed
			failedMu.Unlock()
			if shouldStop {
				return
			}
		}

		task, ok := o.nextTask(ctx, hostName, taskQueue, isFirstTask)
		if !ok {
			return // Queue closed or run cancelled
		}

		// A task pinned to other hosts goes back to the queue for a worker
		// that may run it. This host stays available; the short pause keeps
		// this worker from racing the allowed host for the same task.
		if !task.AllowsHost(hostName) {
			if !bounceTask(ctx, task, requeueChan) {
				return
			}
			continue
		}

		// Execute the task
		taskStart := time.Now()
		result, requeue := worker.executeTaskWithRequeue(ctx, task)

		if requeue {
			// Notify about re-queue
			if o.outputMgr != nil {
				o.outputMgr.TaskRequeued(task.Name, task.Index, hostName)
			}

			// Send task back to the queue BEFORE marking host unavailable.
			// This ordering matters: the dispatcher checks allHostsUnavailable()
			// and drains requeueChan. If we marked unavailable first, the
			// dispatcher could see all hosts down before the task is in the channel.
			select {
			case requeueChan <- task:
			case <-ctx.Done():
				return
			}

			// Now mark the host unavailable
			o.markHostUnavailable(hostName)

			// This worker is done (host unavailable)
			return
		}

		resultChan <- result

		// Record first-task duration for performance tracking
		if isFirstTask {
			o.recordFirstTaskTime(hostName, time.Since(taskStart))
			isFirstTask = false
		}

		// Update failed flag for fail-fast
		if !result.Success() && o.config.FailFast {
			failedMu.Lock()
			*failed = true
			failedMu.Unlock()
			// Cancel remaining tasks
			o.cancelOnce.Do(func() {
				if o.cancelFunc != nil {
					o.cancelFunc()
				}
			})
		}
	}
}

// buildResult constructs the final Result from collected task results.
func (o *Orchestrator) buildResult(duration time.Duration, hostsUsed map[string]bool) *Result {
	o.resultsMu.Lock()
	defer o.resultsMu.Unlock()

	result := &Result{
		TaskResults: o.results,
		Duration:    duration,
		HostsUsed:   make([]string, 0, len(hostsUsed)),
	}

	for host := range hostsUsed {
		result.HostsUsed = append(result.HostsUsed, host)
	}

	for i := range o.results {
		if o.results[i].Success() {
			result.Passed++
		} else {
			result.Failed++
		}
	}

	return result
}

// markHostSynced marks a host as synced and returns whether it was already synced.
func (o *Orchestrator) markHostSynced(hostName string) bool {
	o.syncMu.Lock()
	defer o.syncMu.Unlock()

	if o.syncedHosts[hostName] {
		return true
	}
	o.syncedHosts[hostName] = true
	return false
}

// checkHostSetup checks if setup has already been attempted for a host.
// Returns (alreadyAttempted, previousError).
// If alreadyAttempted is true, previousError contains any error from the prior attempt.
func (o *Orchestrator) checkHostSetup(hostName string) (bool, error) {
	o.setupMu.Lock()
	defer o.setupMu.Unlock()

	if o.setupHosts[hostName] {
		return true, o.setupErrors[hostName]
	}
	return false, nil
}

// recordHostSetup records the result of a setup attempt for a host.
// Call this after setup completes (successfully or with error).
func (o *Orchestrator) recordHostSetup(hostName string, err error) {
	o.setupMu.Lock()
	defer o.setupMu.Unlock()

	o.setupHosts[hostName] = true
	o.setupErrors[hostName] = err
}

// markHostUnavailable marks a host as unavailable (e.g., SSH connection failed).
// Tasks assigned to unavailable hosts are re-queued for other hosts.
func (o *Orchestrator) markHostUnavailable(hostName string) {
	o.unavailableMu.Lock()
	defer o.unavailableMu.Unlock()
	o.unavailableHosts[hostName] = true
}

// isHostUnavailable checks if a host has been marked unavailable.
func (o *Orchestrator) isHostUnavailable(hostName string) bool {
	o.unavailableMu.Lock()
	defer o.unavailableMu.Unlock()
	return o.unavailableHosts[hostName]
}

// allHostsUnavailable returns true if all configured hosts are unavailable.
func (o *Orchestrator) allHostsUnavailable() bool {
	o.unavailableMu.Lock()
	defer o.unavailableMu.Unlock()

	for _, hostName := range o.hostList {
		if !o.unavailableHosts[hostName] {
			return false
		}
	}
	return true
}

// bounceBackoff is how long a worker waits after handing back a task it is
// not allowed to run, so the allowed host's worker wins the next read.
const bounceBackoff = 50 * time.Millisecond

// nextTask pulls the next task for this worker. Subsequent tasks on a slow
// host wait out getSlowHostDelay first, but only when nothing is already
// queued, so fast hosts get first claim without idling the slow one. ok is
// false when the queue closed or the run was cancelled.
func (o *Orchestrator) nextTask(ctx context.Context, hostName string, taskQueue <-chan TaskInfo, isFirstTask bool) (TaskInfo, bool) {
	if !isFirstTask {
		select {
		case task, ok := <-taskQueue:
			return task, ok
		default:
		}
		if delay := o.getSlowHostDelay(hostName); delay > 0 {
			select {
			case <-ctx.Done():
				return TaskInfo{}, false
			case <-time.After(delay):
			}
		}
	}
	select {
	case task, ok := <-taskQueue:
		return task, ok
	case <-ctx.Done():
		return TaskInfo{}, false
	}
}

// bounceTask returns a task this worker may not run to the dispatcher, then
// pauses so the allowed host's worker wins the next read. It reports false
// when the run was cancelled and the worker should exit.
func bounceTask(ctx context.Context, task TaskInfo, requeueChan chan<- TaskInfo) bool {
	select {
	case requeueChan <- task:
	case <-ctx.Done():
		return false
	}
	select {
	case <-time.After(bounceBackoff):
	case <-ctx.Done():
		return false
	}
	return true
}

// pickWorkerHosts returns the hosts that get a worker goroutine: the first n
// in priority order, plus any further host a restricted task needs. Returns
// hostList order, deduplicated.
func (o *Orchestrator) pickWorkerHosts(n int) []string {
	if n > len(o.hostList) {
		n = len(o.hostList)
	}
	picked := make([]string, 0, n)
	picked = append(picked, o.hostList[:n]...)
	has := func(name string) bool {
		for _, p := range picked {
			if p == name {
				return true
			}
		}
		return false
	}
	for _, task := range o.tasks {
		if len(task.AllowedHosts) == 0 {
			continue
		}
		covered := false
		for _, p := range picked {
			if task.AllowsHost(p) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		for _, h := range o.hostList {
			if task.AllowsHost(h) && !has(h) {
				picked = append(picked, h)
				break
			}
		}
	}
	return picked
}

// hasAvailableHostFor reports whether some worker host that the task allows
// is still available.
func (o *Orchestrator) hasAvailableHostFor(task TaskInfo) bool {
	hosts := o.workerHosts
	if len(hosts) == 0 {
		hosts = o.hostList
	}
	for _, h := range hosts {
		if task.AllowsHost(h) && !o.isHostUnavailable(h) {
			return true
		}
	}
	return false
}

// noHostResult is the failure recorded when no live host may run a task.
func noHostResult(task TaskInfo) TaskResult {
	err := fmt.Errorf("all hosts unavailable")
	if len(task.AllowedHosts) > 0 {
		err = fmt.Errorf("no available host allowed for this task (hosts: %v)", task.AllowedHosts)
	}
	now := time.Now()
	return TaskResult{
		TaskName:  task.Name,
		TaskIndex: task.Index,
		Command:   task.Command,
		Host:      "none",
		ExitCode:  1,
		Error:     err,
		StartTime: now,
		EndTime:   now,
	}
}

// drainUnconsumedTasks pulls any remaining tasks from taskQueue and sends
// failure results for them. Called when the dispatcher exits and all hosts
// are unavailable, since no workers remain to consume queued tasks.
func (o *Orchestrator) drainUnconsumedTasks(taskQueue <-chan TaskInfo, resultChan chan<- TaskResult) {
	if !o.allHostsUnavailable() {
		return
	}
	for {
		select {
		case task := <-taskQueue:
			resultChan <- TaskResult{
				TaskName:  task.Name,
				TaskIndex: task.Index,
				Command:   task.Command,
				Host:      "none",
				ExitCode:  1,
				Error:     fmt.Errorf("all hosts unavailable"),
				StartTime: time.Now(),
				EndTime:   time.Now(),
			}
		default:
			return
		}
	}
}

// recordFirstTaskTime records the duration of the first task completed by a host.
// This is used to identify slow hosts and optimize work distribution.
func (o *Orchestrator) recordFirstTaskTime(hostName string, duration time.Duration) {
	o.hostTimeMu.Lock()
	defer o.hostTimeMu.Unlock()

	// Only record if this is the first task for this host
	if _, exists := o.hostFirstTaskTime[hostName]; exists {
		return
	}

	o.hostFirstTaskTime[hostName] = duration

	// Track the fastest first-task completion
	if o.fastestFirstTask == 0 || duration < o.fastestFirstTask {
		o.fastestFirstTask = duration
	}
}

// getSlowHostDelay returns a delay that slow hosts should wait before grabbing
// additional tasks. This gives fast hosts a chance to grab tasks first.
//
// The delay is based on how much slower this host was compared to the fastest:
// - If host took 2x as long as fastest, delay is 100% of fastest task time
// - If host took 1.5x as long, delay is 50% of fastest task time
// - If host is within 10% of fastest, no delay
//
// This delay only applies after the first task (when we have performance data).
func (o *Orchestrator) getSlowHostDelay(hostName string) time.Duration {
	o.hostTimeMu.Lock()
	defer o.hostTimeMu.Unlock()

	hostTime, exists := o.hostFirstTaskTime[hostName]
	if !exists || o.fastestFirstTask == 0 {
		return 0 // No data yet, no delay
	}

	// Calculate slowdown ratio
	ratio := float64(hostTime) / float64(o.fastestFirstTask)

	// No delay if within 10% of fastest
	if ratio < 1.1 {
		return 0
	}

	// Delay proportional to how slow the host is.
	// The delay should be long enough that fast hosts can finish their
	// current task and grab from the queue before the slow host does.
	//
	// For a host that's 1.5x slower: delay = (1.5 - 1.0) * fastest = 50% of fastest
	// For a host that's 2x slower: delay = (2.0 - 1.0) * fastest = 100% of fastest
	//
	// This gives fast hosts time to complete their current task and grab more work.
	delayFactor := ratio - 1.0
	if delayFactor > 1.0 {
		delayFactor = 1.0 // Cap at 100% of fastest time
	}

	return time.Duration(float64(o.fastestFirstTask) * delayFactor)
}

// GetOutputManager returns the output manager for external access.
func (o *Orchestrator) GetOutputManager() *OutputManager {
	return o.outputMgr
}

// runLocal executes tasks locally (sequentially) when no remote hosts are configured.
func (o *Orchestrator) runLocal(ctx context.Context) (*Result, error) {
	// Determine TTY status for output manager
	isTTY := isTerminal()

	// Initialize output manager
	o.outputMgr = NewOutputManager(o.config.OutputMode, isTTY)
	defer o.outputMgr.Close()

	// Show all tasks as pending upfront (pass full TaskInfo for Index tracking)
	o.outputMgr.InitTasks(o.tasks)

	startTime := time.Now()

	// Create a local worker
	worker := &localWorker{
		orchestrator: o,
	}

	// Execute tasks sequentially
	for _, task := range o.tasks {
		// Check for cancellation
		if ctx.Err() != nil {
			break
		}

		result := worker.executeTask(ctx, task)
		o.results = append(o.results, result)

		// Check fail-fast
		if !result.Success() && o.config.FailFast {
			break
		}
	}

	// Build final result
	duration := time.Since(startTime)
	return o.buildResult(duration, map[string]bool{"local": true}), nil
}
