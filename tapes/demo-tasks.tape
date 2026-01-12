# VHS Demo for rr named tasks
#
# Shows running named tasks defined in .rr.yaml config.
#
# Record: vhs tapes/demo-tasks.tape
# Output: tapes/demo-tasks.gif

Output tapes/demo-tasks.gif

# Visual settings
Set FontSize 16
Set Width 850
Set Height 500
Set FontFamily "Menlo"
Set Theme "Catppuccin Mocha"
Set Padding 20
Set TypingSpeed 35ms
Set WindowBar Colorful
Set CursorBlink false


Sleep 300ms

# Show task definitions in config
Type "grep -A8 'tasks:' .rr.yaml"
Sleep 200ms
Enter
Sleep 2000ms

# Show help with task commands
Type "rr --help | tail -15"
Sleep 200ms
Enter
Sleep 2000ms

# Run a named task
Type "rr test"
Sleep 300ms
Enter

# Wait for task execution
Sleep 4500ms

# End
Sleep 600ms
