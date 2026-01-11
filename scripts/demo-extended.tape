# VHS Extended Demo Script for rr
#
# This version shows more features including the monitor TUI.
# Use demo.tape for a shorter, punchier GIF.
#
# Record with: vhs scripts/demo-extended.tape
# Install VHS: brew install charmbracelet/tap/vhs

Output demo-extended.gif

# Visual settings
Set FontSize 16
Set Width 1000
Set Height 650
Set Theme "Catppuccin Mocha"
Set Padding 20
Set TypingSpeed 35ms
Set WindowBar Colorful
Set CursorBlink false

Sleep 300ms

# Scene 1: The main workflow
Type "rr run 'make test'"
Sleep 200ms
Enter

# Workflow phases: connect -> sync -> lock -> execute
Sleep 7s

# Scene 2: Quick exec without sync
Type "rr exec 'df -h /'"
Sleep 200ms
Enter
Sleep 2s

# Scene 3: Check status
Type "rr status"
Sleep 200ms
Enter
Sleep 2500ms

# Scene 4: Monitor dashboard (the visual showpiece)
Type "rr monitor"
Sleep 200ms
Enter

# Let the TUI render and show some data
Sleep 4s

# Exit monitor
Type "q"
Sleep 500ms

# Scene 5: Doctor for troubleshooting
Type "rr doctor"
Sleep 200ms
Enter
Sleep 3s

Sleep 1s
