# VHS Hero Demo for rr
#
# The main demo GIF for the README - shows the core workflow.
#
# Record: vhs tapes/demo.tape
# Output: tapes/demo.gif
#
# Prerequisites:
# - VHS installed: brew install charmbracelet/tap/vhs
# - For mock recording: uses tapes/mock/rr-mock
# - For real recording: rr installed with configured .rr.yaml

Output tapes/demo.gif

# Visual settings - clean and readable
Set FontSize 18
Set Width 800
Set Height 500
Set FontFamily "JetBrains Mono"
Set Theme "Catppuccin Mocha"
Set Padding 24
Set TypingSpeed 40ms
Set WindowBar Colorful
Set CursorBlink false

# Use mock environment for consistent output
Env PATH "./tapes/mock:$PATH"

# Let terminal settle
Sleep 300ms

# The hero shot: sync + run in one command
Type "rr run 'make test'"
Sleep 300ms
Enter

# Wait for the mock workflow output
# Mock has built-in timing for phases
Sleep 4500ms

# Quick follow-up showing exec (no sync, faster)
Type "rr exec 'uptime'"
Sleep 200ms
Enter

# Wait for exec output
Sleep 2000ms

# Breathing room at end
Sleep 800ms
