# VHS Demo for rr host failover
#
# Shows automatic connection failover when primary host is unavailable.
#
# Record: vhs tapes/demo-failover.tape
# Output: tapes/demo-failover.gif

Output tapes/demo-failover.gif

# Visual settings
Set FontSize 16
Set Width 850
Set Height 450
Set FontFamily "JetBrains Mono"
Set Theme "Catppuccin Mocha"
Set Padding 20
Set TypingSpeed 35ms
Set WindowBar Colorful
Set CursorBlink false

# Use mock environment
Env PATH "./tapes/mock:$PATH"

Sleep 300ms

# Show the multi-host config
Type "head -12 .rr.yaml"
Sleep 200ms
Enter
Sleep 2000ms

# Run a command - will show failover from LAN to VPN
Type "rr run --failover 'echo hello from remote'"
Sleep 300ms
Enter

# Wait for failover sequence (mock shows timeout -> success)
Sleep 4500ms

# End
Sleep 600ms
