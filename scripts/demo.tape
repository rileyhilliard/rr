# VHS Demo Script for rr
#
# Record with: vhs scripts/demo.tape
# Install VHS: brew install charmbracelet/tap/vhs
# Docs: https://github.com/charmbracelet/vhs
#
# Prerequisites:
# - rr installed and in PATH
# - Working .rr.yaml with configured host
# - SSH access to the remote

Output demo.gif

# Visual settings - clean and readable
Set FontSize 18
Set Width 800
Set Height 500
Set Theme "Catppuccin Mocha"
Set Padding 24
Set TypingSpeed 40ms
Set WindowBar Colorful
Set CursorBlink false

# Let terminal settle
Sleep 300ms

# The hero shot: sync + run in one command
Type "rr run 'make test'"
Sleep 300ms
Enter

# Wait for the workflow: connect -> sync -> lock -> execute
# Adjust timing based on actual execution speed
Sleep 6s

# Quick follow-up showing exec (no sync)
Type "rr exec 'uptime'"
Sleep 200ms
Enter
Sleep 2500ms

# End with a bit of breathing room
Sleep 1s
