# VHS Demo for rr doctor
#
# Shows the diagnostic checks and troubleshooting output.
#
# Record: vhs tapes/demo-doctor.tape
# Output: tapes/demo-doctor.gif

Output tapes/demo-doctor.gif

# Visual settings (1.5x for crisp output)
Set FontSize 24
Set Width 1400
Set Height 850
Set FontFamily "Menlo"
Set Theme "Catppuccin Mocha"
Set Padding 30
Set TypingSpeed 35ms
Set WindowBar Colorful
Set CursorBlink false

Sleep 300ms

# Run diagnostics
Type "rr doctor"
Sleep 300ms
Enter

# Wait for all checks to run (mock has built-in timing)
Sleep 5500ms

# End
Sleep 800ms
