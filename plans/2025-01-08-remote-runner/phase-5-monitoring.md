# Phase 5: Host Monitoring Dashboard

> **Status:** NOT_STARTED

## Goal

Build a real-time terminal dashboard (`rr monitor`) showing system metrics across all configured hosts. Think btop/htop, but for your fleet of worker machines.

## Success Criteria

- [ ] `rr monitor` shows all hosts with CPU, RAM, network
- [ ] GPU metrics shown when available (NVIDIA)
- [ ] Real-time updates at configurable interval
- [ ] Graceful handling of unreachable hosts
- [ ] Keyboard navigation (sort, select, SSH into host)
- [ ] Responsive layout for different terminal sizes
- [ ] Sparkline history graphs

## Phase Exit Criteria

- [ ] All 8 tasks completed
- [ ] `make test` passes with >70% coverage on new code
- [ ] Manual testing on at least 2 different hosts
- [ ] Works on macOS and Linux remote hosts

## Context Loading

```bash
# Read before starting:
read internal/host/selector.go
read internal/ui/spinner.go
read internal/ui/colors.go

# Reference:
read ../../ARCHITECTURE.md               # Lines 1177-1658 for full monitor design
```

---

## Execution Order

```
┌─────────────────────────────────────────────────────────────────┐
│ Task 1: Metric Types & Platform Parsers                         │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Task 2: Metrics Collector with Connection Pooling               │
└─────────────────────────────────────────────────────────────────┘
                              │
          ┌───────────────────┴───────────────────┐
          ▼                                       ▼
┌─────────────────────────────┐     ┌─────────────────────────────┐
│ Task 3: TUI Model & View    │     │ Task 4: Sparklines &        │
│ (parallel)                  │     │ Progress Bars               │
└─────────────────────────────┘     └─────────────────────────────┘
          │                                       │
          └───────────────────┬───────────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Task 5: Keyboard Navigation & Sorting                           │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ Task 6: Responsive Layouts                                      │
└─────────────────────────────────────────────────────────────────┘
                              │
          ┌───────────────────┴───────────────────┐
          ▼                                       ▼
┌─────────────────────────────┐     ┌─────────────────────────────┐
│ Task 7: CLI Command &       │     │ Task 8: Config & Testing    │
│ Expanded Host View          │     │                             │
└─────────────────────────────┘     └─────────────────────────────┘
```

---

## Tasks

### Task 1: Metric Types & Platform Parsers

**Context:**
- Create: `internal/monitor/types.go`, `internal/monitor/parsers/linux.go`, `internal/monitor/parsers/darwin.go`, `internal/monitor/parsers/gpu.go`
- Reference: `../../ARCHITECTURE.md` lines 1487-1499 for metrics sources

**Steps:**

1. [ ] Create `internal/monitor/types.go`:
   ```go
   type HostMetrics struct {
       Timestamp time.Time
       CPU       CPUMetrics
       RAM       RAMMetrics
       GPU       *GPUMetrics  // nil if no GPU
       Network   []NetworkInterface
       System    SystemInfo
   }

   type CPUMetrics struct {
       Percent   float64
       Cores     int
       LoadAvg   [3]float64
   }

   type RAMMetrics struct {
       UsedBytes  int64
       TotalBytes int64
       Cached     int64
       Available  int64
   }

   type GPUMetrics struct {
       Name        string
       Percent     float64
       MemoryUsed  int64
       MemoryTotal int64
       Temperature int
       PowerWatts  int
   }
   ```

2. [ ] Create `internal/monitor/parsers/linux.go`:
   - `ParseLinuxCPU(output string) (*CPUMetrics, error)` - parse `/proc/stat`
   - `ParseLinuxMemory(output string) (*RAMMetrics, error)` - parse `/proc/meminfo`
   - `ParseLinuxNetwork(output string) ([]NetworkInterface, error)` - parse `/proc/net/dev`

3. [ ] Create `internal/monitor/parsers/darwin.go`:
   - `ParseDarwinCPU(output string) (*CPUMetrics, error)` - parse `top -l 1 -n 0`
   - `ParseDarwinMemory(output string) (*RAMMetrics, error)` - parse `vm_stat`
   - `ParseDarwinNetwork(output string) ([]NetworkInterface, error)` - parse `netstat -ib`

4. [ ] Create `internal/monitor/parsers/gpu.go`:
   - `ParseNvidiaSMI(output string) (*GPUMetrics, error)` - parse nvidia-smi CSV output
   - Handle missing GPU gracefully (return nil, not error)

5. [ ] Create tests for each parser with sample output

**Verify:** `go test ./internal/monitor/parsers/...`

---

### Task 2: Metrics Collector with Connection Pooling

**Context:**
- Create: `internal/monitor/collector.go`, `internal/monitor/pool.go`, `internal/monitor/command.go`, `internal/monitor/history.go`
- Read: `pkg/sshutil/client.go` (SSH client)
- Reference: `../../ARCHITECTURE.md` lines 1503-1581 for collection strategy

**Steps:**

1. [ ] Create `internal/monitor/command.go`:
   - Build batched metrics command per platform:
     - Linux: `cat /proc/stat /proc/meminfo /proc/net/dev 2>/dev/null; nvidia-smi --query-gpu=... 2>/dev/null || true`
     - macOS: `top -l 1 -n 0; vm_stat; netstat -ib`
   - Single SSH exec per host per refresh

2. [ ] Create `internal/monitor/pool.go`:
   - SSH connection pool: keep connections alive between refreshes
   - `Pool` struct with `Get(host)`, `Return(host, conn)`, `Close(host)`
   - Reconnect on failure with exponential backoff
   - Thread-safe

3. [ ] Create `internal/monitor/collector.go`:
   - `Collector` struct with pool and parsers
   - `Collect(hosts []Host) map[string]*HostMetrics`
   - Parallel collection across hosts using goroutines
   - Timeout handling per host (don't block others)
   - Detect platform on first connection

4. [ ] Create `internal/monitor/history.go`:
   - Ring buffer for sparkline history (last 60 data points)
   - `History` struct managing per-host, per-metric histories
   - `Push(host, metrics)`, `Get(host, metric, count)`
   - Thread-safe

5. [ ] Create tests for collector and pool

**Verify:** `go test ./internal/monitor/...`

---

### Task 3: TUI Model & View

**Context:**
- Create: `internal/monitor/model.go`, `internal/monitor/view.go`, `internal/monitor/styles.go`
- Read: `internal/ui/colors.go` (color palette)
- Reference: `../../ARCHITECTURE.md` lines 1259-1377 for layout design

**Steps:**

1. [ ] Create `internal/monitor/styles.go`:
   - Lip Gloss styles per ARCHITECTURE.md color palette:
     - Background: `#0d1117`, Surface: `#161b22`, Border: `#30363d`
     - Semantic: healthy (`#3fb950`), warning (`#d29922`), critical (`#f85149`)
   - Threshold-based color functions

2. [ ] Create `internal/monitor/model.go`:
   - Bubble Tea model for dashboard
   - State: hosts, metrics, selected host, view mode (list/detail), sort order
   - Messages: tick, metrics update, key press, resize

3. [ ] Create `internal/monitor/view.go`:
   - Main view function rendering host cards
   - Header: "rr monitor | X hosts | ● Y online | ↻ Zs ago"
   - Host cards with metrics (CPU, RAM, GPU, Network bars)
   - Footer with keyboard hints
   - Reference ARCHITECTURE.md lines 1262-1302 for exact layout

4. [ ] Create `internal/monitor/card.go`:
   - Host card component
   - State indicators: connected (●), slow (●), unreachable (○)
   - Metrics display with bars and sparklines

**Verify:** Visual inspection with `go run ./cmd/rr monitor`

---

### Task 4: Sparklines & Progress Bars

**Context:**
- Create: `internal/ui/sparkline.go`, update `internal/ui/progress.go`
- Reference: `../../ARCHITECTURE.md` lines 1380-1406 for visual elements

**Steps:**

1. [ ] Create `internal/ui/sparkline.go`:
   - `RenderSparkline(data []float64, width int) string`
   - Use block characters: ▁▂▃▄▅▆▇█ (8 levels)
   - Color based on current value threshold
   - Handle empty data gracefully

2. [ ] Update `internal/ui/progress.go`:
   - `RenderProgressBar(percent float64, width int, thresholds Thresholds) string`
   - Color based on threshold: green (0-60%), amber (60-80%), red (80-100%)
   - Show percentage value at end
   - Use filled/empty block characters

3. [ ] Integrate into host cards

4. [ ] Add tests for rendering edge cases

**Verify:** Visual inspection of sparklines and progress bars

---

### Task 5: Keyboard Navigation & Sorting

**Context:**
- Modify: `internal/monitor/model.go`
- Create: `internal/monitor/keybindings.go`, `internal/monitor/help.go`
- Reference: `../../ARCHITECTURE.md` lines 1592-1602 for keybindings

**Steps:**

1. [ ] Create `internal/monitor/keybindings.go`:
   - Handle keys: `q`/`Ctrl+C` quit, `r` refresh, `s` sort, `↑↓` select, `Enter` SSH, `?` help
   - Return appropriate Bubble Tea commands

2. [ ] Update `internal/monitor/model.go`:
   - Track selected host index
   - Track sort order: name, CPU, RAM, GPU
   - Handle key events

3. [ ] Create `internal/monitor/help.go`:
   - Help overlay component
   - Show all keyboard shortcuts
   - Toggle with `?` key

4. [ ] Implement SSH into selected host:
   - On `Enter`, open new terminal with `ssh <host-alias>`
   - Use `os/exec` to spawn terminal

**Verify:** Interactive testing of keyboard navigation

---

### Task 6: Responsive Layouts

**Context:**
- Modify: `internal/monitor/view.go`
- Reference: `../../ARCHITECTURE.md` lines 1448-1455 for breakpoints

**Steps:**

1. [ ] Update `internal/monitor/view.go`:
   - Detect terminal size on start and resize events
   - Implement layouts:
     - <80 cols: Minimal (metrics only, no graphs)
     - 80-120 cols: Compact (inline graphs, abbreviated labels)
     - 120+ cols: Full (expanded cards, detailed graphs)
     - 160+ cols: Two-column layout

2. [ ] Handle height constraints:
   - <24 rows: Hide footer help
   - 24-40 rows: Standard layout
   - 40+ rows: Taller graphs

3. [ ] Handle terminal resize events:
   - Re-render on resize message
   - Smooth transition

**Verify:** Resize terminal while running `rr monitor`

---

### Task 7: CLI Command & Expanded Host View

**Context:**
- Create: `internal/cli/monitor.go`, `internal/monitor/detail.go`

**Steps:**

1. [ ] Create `internal/cli/monitor.go`:
   - Load config
   - Parse flags: `--hosts`, `--interval`, `--no-gpu`, `--quiet`
   - Initialize collector
   - Run Bubble Tea program
   - Handle graceful shutdown (close connections)

2. [ ] Create `internal/monitor/detail.go`:
   - Single-host expanded view (on Tab/Enter)
   - Full-width CPU graph with history
   - Memory breakdown (RAM, swap, cached)
   - GPU details (compute, VRAM, temp, power)
   - Per-interface network stats
   - System info (OS, kernel, uptime)

3. [ ] Add Esc to return to list view

**Verify:**
```bash
./rr monitor
./rr monitor --hosts=mini
./rr monitor --interval=1s
./rr monitor --no-gpu
```

---

### Task 8: Config & Testing

**Context:**
- Modify: `internal/config/types.go`
- Create: `tests/integration/monitor_test.go`

**Steps:**

1. [ ] Update `internal/config/types.go`:
   - Add `MonitorConfig`:
     ```go
     type MonitorConfig struct {
         Interval   time.Duration
         Thresholds ThresholdConfig
         Exclude    []string
         GPUTimeout time.Duration
     }
     ```

2. [ ] Apply thresholds in view for color coding

3. [ ] Create integration tests:
   - Test collector against localhost
   - Test parser outputs
   - Test model state transitions

4. [ ] Manual testing on different platforms:
   - macOS host
   - Linux host
   - Host with GPU

**Verify:**
```bash
cat >> .rr.yaml << 'EOF'
monitor:
  interval: 2s
  thresholds:
    cpu:
      warning: 70
      critical: 90
    ram:
      warning: 80
      critical: 95
EOF

./rr monitor
```

---

## Verification

After all tasks complete:

```bash
# Basic monitoring
./rr monitor

# Monitor specific hosts
./rr monitor --hosts=mini

# Fast refresh
./rr monitor --interval=1s

# Test keyboard navigation
# - Press s to sort
# - Press ↓ to select
# - Press Enter to expand
# - Press Esc to collapse
# - Press q to quit

# Test with unreachable host (should show gracefully)
# Test responsive layout (resize terminal)
```
