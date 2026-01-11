# VHS Demo Tapes

Terminal recordings for the rr README and documentation.

## Prerequisites

Install VHS (terminal recorder):

```bash
brew install charmbracelet/tap/vhs
```

Ensure JetBrains Mono font is available (or adjust tapes to use your preferred font).

## Recording

**Record all demos:**

```bash
make demos
```

**Record individual demo:**

```bash
vhs tapes/demo.tape
```

## Mock Environment

The `mock/` directory contains tools for recording demos without live SSH:

- `mock/rr-mock` - Shell script that simulates rr output
- `mock/.rr.yaml` - Sample configuration for demos

To use the mock:

```bash
# Add mock directory to PATH before recording
PATH="$(pwd)/tapes/mock:$PATH" vhs tapes/demo.tape
```

Or update the tape to explicitly call the mock:

```tape
Set Shell "bash"
Set Env { "PATH": "./tapes/mock:$PATH" }
```

## Available Tapes

| Tape | Description | Duration | Output |
|------|-------------|----------|--------|
| `demo.tape` | Hero shot: run + exec | ~8s | `demo.gif` |
| `demo-extended.tape` | Full feature showcase | ~18s | `demo-extended.gif` |
| `demo-monitor.tape` | TUI dashboard | ~20s | `demo-monitor.gif` |
| `demo-init.tape` | First-time setup | ~12s | `demo-init.gif` |
| `demo-failover.tape` | Host failover | ~10s | `demo-failover.gif` |
| `demo-tasks.tape` | Named tasks | ~10s | `demo-tasks.gif` |
| `demo-doctor.tape` | Diagnostics | ~12s | `demo-doctor.gif` |

## Style Guide

All tapes use consistent settings:

```tape
Set Shell "bash"
Set FontFamily "JetBrains Mono"
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

The mock environment produces deterministic output for consistent demos. For authentic recordings with actual SSH:

1. Ensure `.rr.yaml` is configured with real hosts
2. Don't add `mock/` to PATH
3. Adjust Sleep timings to match actual execution speed

## Troubleshooting

**Colors look wrong:**
VHS requires a terminal with 24-bit color support. The Catppuccin Mocha theme works best.

**Timing is off:**
Adjust `Sleep` durations in tape files. Mock output has fixed delays; real SSH varies.

**Font not found:**
Install JetBrains Mono or change `Set FontFamily` to a font you have installed.

**GIF too large:**
Reduce dimensions or duration. Consider using `Set Quality` to adjust compression.
