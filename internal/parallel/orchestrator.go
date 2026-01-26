package parallel

import (
	"context"
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
	// The channel is sized to hold all tasks, filled immediately, then closed.
	// Workers range over this channel, naturally competing for work.
	taskQueue := make(chan TaskInfo, len(o.tasks))
	for _, task := range o.tasks {
		taskQueue <- task
	}
	close(taskQueue)

	// Determine number of workers
	numWorkers := len(o.hostList)
	if o.config.MaxParallel > 0 && o.config.MaxParallel < numWorkers {
		numWorkers = o.config.MaxParallel
	}
	if numWorkers > len(o.tasks) {
		numWorkers = len(o.tasks)
	}

	// Result channel for collecting task results
	resultChan := make(chan TaskResult, len(o.tasks))

	// Failed flag for fail-fast mode
	var failed bool
	var failedMu sync.Mutex

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		hostName := o.hostList[i%len(o.hostList)]
		wg.Add(1)
		go func(hostName string) {
			defer wg.Done()
			o.hostWorker(ctx, hostName, taskQueue, resultChan, &failed, &failedMu)
		}(hostName)
	}

	// Collect results in a separate goroutine
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Gather results
	hostsUsed := make(map[string]bool)
	for result := range resultChan {
		o.resultsMu.Lock()
		o.results = append(o.results, result)
		o.resultsMu.Unlock()
		hostsUsed[result.Host] = true
	}

	// Build final result
	duration := time.Since(startTime)
	return o.buildResult(duration, hostsUsed), nil
}

// hostWorker is a goroutine that grabs tasks from the queue and executes them.
func (o *Orchestrator) hostWorker(
	ctx context.Context,
	hostName string,
	taskQueue <-chan TaskInfo,
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

		// Try to grab a task from the queue.
		// For subsequent tasks on slow hosts, we apply a delay to give fast hosts
		// priority. But first, check if a task is immediately available (non-blocking).
		// Only apply the delay if the queue isn't empty and we'd be competing.
		var task TaskInfo
		var ok bool

		if !isFirstTask {
			// Non-blocking check: is a task immediately available?
			select {
			case task, ok = <-taskQueue:
				if !ok {
					return // Queue closed, no more tasks
				}
				// Got a task immediately, skip delay and process it
			default:
				// No task immediately available. Apply slow host delay if needed,
				// then do a blocking read.
				if delay := o.getSlowHostDelay(hostName); delay > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(delay):
					}
				}
				// Blocking read after delay
				task, ok = <-taskQueue
				if !ok {
					return // Queue closed, no more tasks
				}
			}
		} else {
			// First task: grab immediately without delay
			task, ok = <-taskQueue
			if !ok {
				return // Queue closed, no more tasks
			}
		}

		// Execute the task
		taskStart := time.Now()
		result := worker.executeTask(ctx, task)
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
