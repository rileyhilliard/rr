# VHS Demo Script for rr
#
# Record with: vhs scripts/demo.tape
# Install VHS: brew install charmbracelet/tap/vhs
# Docs: https://github.com/charmbracelet/vhs

Output demo.gif

Set FontSize 14
Set Width 800
Set Height 500
Set Theme "Catppuccin Mocha"

Type "# Sync code and run tests on a remote machine"
Enter
Sleep 1s

Type "rr run 'make test'"
Enter
Sleep 500ms

# Simulate output - customize based on actual rr output
# The actual recording should show real rr output

Sleep 3s

Type "# That's it! Files synced, tests ran remotely."
Enter
Sleep 2s
