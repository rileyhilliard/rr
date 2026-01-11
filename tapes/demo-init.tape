# VHS Demo for rr init
#
# Shows the first-time setup experience with rr init.
#
# Record: vhs tapes/demo-init.tape
# Output: tapes/demo-init.gif

Output tapes/demo-init.gif

# Visual settings - taller for form prompts
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

# Show there's no config yet
Type "ls .rr.yaml"
Sleep 200ms
Enter
Sleep 800ms

# Initialize rr
Type "rr init"
Sleep 300ms
Enter

# Wait for interactive prompts (mock has built-in timing)
Sleep 5000ms

# Show the generated config
Type "cat .rr.yaml | head -20"
Sleep 200ms
Enter
Sleep 2500ms

# End
Sleep 600ms
