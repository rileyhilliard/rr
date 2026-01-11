# VHS Demo for rr monitor
#
# Showcases the real-time monitoring TUI dashboard.
#
# Record: vhs tapes/demo-monitor.tape
# Output: tapes/demo-monitor.gif
#
# NOTE: This demo works best with the real rr binary and configured hosts.
# The mock provides a static frame; real rr shows live metrics and animation.
#
# Prerequisites:
# - rr installed and in PATH
# - .rr.yaml with at least one configured host
# - SSH access to remotes

Output tapes/demo-monitor.gif

# Visual settings - wider to show TUI properly
Set FontSize 15
Set Width 1100
Set Height 700
Set FontFamily "JetBrains Mono"
Set Theme "Catppuccin Mocha"
Set Padding 16
Set TypingSpeed 40ms
Set WindowBar Colorful
Set CursorBlink false

Sleep 300ms

# Launch the monitor
Type "rr monitor"
Sleep 200ms
Enter

# Let the TUI initialize and fetch initial metrics
Sleep 3s

# Navigate down to show selection works
Down
Sleep 600ms
Down
Sleep 600ms

# Cycle through sort options (name -> CPU -> RAM)
Type "s"
Sleep 800ms
Type "s"
Sleep 800ms

# Show the help overlay
Type "?"
Sleep 2s

# Close help
Escape
Sleep 600ms

# Navigate up and expand detail view
Up
Sleep 400ms
Enter

# Let the detail view render with graphs
Sleep 2500ms

# Scroll in detail view
Down
Sleep 300ms
Down
Sleep 300ms

# Return to list view
Escape
Sleep 800ms

# Quick refresh
Type "r"
Sleep 1200ms

# Exit cleanly
Type "q"
Sleep 400ms
