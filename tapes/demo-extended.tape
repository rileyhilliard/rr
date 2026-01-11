# VHS Extended Demo for rr
#
# Full feature showcase: run -> exec -> status -> monitor -> doctor
#
# Record: vhs tapes/demo-extended.tape
# Output: tapes/demo-extended.gif
#
# This version shows more features. Use demo.tape for a shorter GIF.

Output tapes/demo-extended.gif

# Visual settings
Set FontSize 16
Set Width 1000
Set Height 650
Set FontFamily "JetBrains Mono"
Set Theme "Catppuccin Mocha"
Set Padding 20
Set TypingSpeed 35ms
Set WindowBar Colorful
Set CursorBlink false

# Use mock environment
Env PATH "./tapes/mock:$PATH"

Sleep 300ms

# Scene 1: The main workflow
Type "rr run 'make test'"
Sleep 200ms
Enter

# Wait for run phases
Sleep 4500ms

# Scene 2: Quick exec without sync
Type "rr exec 'df -h /'"
Sleep 200ms
Enter
Sleep 1800ms

# Scene 3: Check status
Type "rr status"
Sleep 200ms
Enter
Sleep 2000ms

# Scene 4: Doctor for diagnostics
Type "rr doctor"
Sleep 200ms
Enter
Sleep 3500ms

# End
Sleep 800ms
