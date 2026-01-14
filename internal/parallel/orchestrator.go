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
		tasks:       tasks,
		hosts:       hosts,
		hostList:    hostList,
		config:      cfg,
		resolved:    resolved,
		syncedHosts: make(map[string]bool),
		results:     make([]TaskResult, 0, len(tasks)),
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

	for task := range taskQueue {
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

		// Execute the task
		result := worker.executeTask(ctx, task)
		resultChan <- result

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
