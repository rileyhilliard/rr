# VHS Demo Script for rr monitor
#
# Showcases the real-time monitoring TUI dashboard.
#
# Record with: vhs scripts/demo-monitor.tape
# Install VHS: brew install charmbracelet/tap/vhs
#
# Prerequisites:
# - rr installed and in PATH
# - Working .rr.yaml with at least one configured host
# - SSH access to the remote(s)

Output demo-monitor.gif

# Visual settings - wider to show the TUI properly
Set FontSize 15
Set Width 1100
Set Height 700
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
Sleep 800ms
Down
Sleep 800ms

# Cycle through sort options (name -> CPU -> RAM -> GPU)
Type "s"
Sleep 1s
Type "s"
Sleep 1s

# Show the help overlay
Type "?"
Sleep 2s

# Close help
Escape
Sleep 800ms

# Select a host and expand to detail view
Up
Sleep 500ms
Enter

# Let the detail view render with graphs
Sleep 3s

# Scroll down in detail view to show more content
Down
Sleep 400ms
Down
Sleep 400ms
Down
Sleep 400ms

# Return to list view
Escape
Sleep 1s

# Quick refresh
Type "r"
Sleep 1500ms

# Exit cleanly
Type "q"
Sleep 500ms
