# VHS Demo for rr doctor
#
# Shows the diagnostic checks and troubleshooting output.
#
# Record: vhs tapes/demo-doctor.tape
# Output: tapes/demo-doctor.gif

Output tapes/demo-doctor.gif

# Visual settings
Set FontSize 16
Set Width 900
Set Height 550
Set FontFamily "JetBrains Mono"
Set Theme "Catppuccin Mocha"
Set Padding 20
Set TypingSpeed 35ms
Set WindowBar Colorful
Set CursorBlink false

# Use mock environment
Env PATH "./tapes/mock:$PATH"

Sleep 300ms

# Run diagnostics
Type "rr doctor"
Sleep 300ms
Enter

# Wait for all checks to run (mock has built-in timing)
Sleep 5500ms

# End
Sleep 800ms
