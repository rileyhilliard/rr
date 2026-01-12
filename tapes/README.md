# VHS Demo Tapes

Terminal recordings for the rr README and documentation.

## Prerequisites

Install VHS (terminal recorder):

```bash
brew install charmbracelet/tap/vhs
```

Tapes use Menlo font (built into macOS). On Linux, install a monospace font and update tapes if needed.

## Recording

**Record all demos with real rr (requires SSH setup):**

```bash
make demos
```

**Record all demos with mock (deterministic, no SSH needed):**

```bash
make demos-mock
```

**Record individual demo:**

```bash
vhs tapes/demo.tape                                    # uses real rr
PATH="$(pwd)/tapes/mock:$PATH" vhs tapes/demo.tape    # uses mock
```

## Mock Environment

The `mock/` directory contains tools for recording demos without live SSH:

- `mock/rr` - Symlink to mock script
- `mock/rr-mock` - Shell script that simulates rr output
- `mock/.rr.yaml` - Sample configuration for demos

The mock produces deterministic output for consistent demo recordings.

## Available Tapes

| Tape | Description | Duration | Output |
|------|-------------|----------|--------|
| `demo.tape` | Hero shot: run + exec | ~8s | `demo.gif` |
| `demo-extended.tape` | Full feature showcase | ~18s | `demo-extended.gif` |
| `demo-monitor.tape` | TUI dashboard (requires real rr) | ~20s | `demo-monitor.gif` |
| `demo-init.tape` | First-time setup (requires real rr) | ~12s | `demo-init.gif` |
| `demo-failover.tape` | Host failover | ~10s | `demo-failover.gif` |
| `demo-tasks.tape` | Named tasks | ~10s | `demo-tasks.gif` |
| `demo-doctor.tape` | Diagnostics | ~12s | `demo-doctor.gif` |

## Style Guide

All tapes use consistent settings:

```tape
Set FontFamily "Menlo"
Set Theme "Catppuccin Mocha"
Set Padding 20
Set WindowBar Colorful
Set CursorBlink false
Set TypingSpeed 35ms
```

**Dimensions by type:**

- Hero demo: 800x500, 18pt
- Extended: 1000x650, 16pt
- TUI/Monitor: 1100x700, 15pt
- Focused features: 850x450, 16pt

## Real vs Mock Recording

- **Mock** (`make demos-mock`): Deterministic output, no SSH needed. Best for CI/consistent demos.
- **Real** (`make demos`): Authentic recordings with actual SSH. Adjust Sleep timings to match execution speed.

## Troubleshooting

**Colors look wrong:**
VHS requires a terminal with 24-bit color support. The Catppuccin Mocha theme works best.

**Timing is off:**
Adjust `Sleep` durations in tape files. Mock output has fixed delays; real SSH varies.

**Font not found:**
Tapes use Menlo (built into macOS). On other systems, change `Set FontFamily` to an available monospace font.

**GIF too large:**
Reduce dimensions or duration. Consider using `Set Quality` to adjust compression.
