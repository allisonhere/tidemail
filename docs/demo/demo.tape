# TideMail README demo. Record it with docs/demo/record.sh (it builds TideMail, seeds a
# throwaway sandbox of made-up mail, then runs this tape). Output: docs/demo/demo.gif.
#
# Keys used: Tab (next pane), Down/Up (move), Enter (open), Ctrl+N (next link), T (theme picker).
# Adjust the Sleep values if your machine is slow.

Output docs/demo/demo.gif

Set Shell "bash"
Set FontSize 15
Set Width 1400
Set Height 820
Set Padding 12
Set Theme "Catppuccin Mocha"
Set TypingSpeed 70ms
Set PlaybackSpeed 1.0

# Point TideMail at the sandbox, never at a real inbox.
Env XDG_CONFIG_HOME "/tmp/tidemail-demo/config"
Env XDG_DATA_HOME "/tmp/tidemail-demo/data"
Env HOME "/tmp/tidemail-demo"
Env PATH "/tmp/tidemail-demo/bin:/usr/local/bin:/usr/bin:/bin"

Type "tidemail"
Enter
Sleep 2.5s

# Move into the message list and browse the unified inbox.
Tab
Sleep 600ms
Down
Sleep 700ms
Down
Sleep 700ms
Up
Sleep 700ms

# Open the newsletter: the LINKS list shows each link with its real destination,
# and leaves the tracking pixel out. Ctrl+N steps through the links.
Down
Down
Sleep 400ms
Enter
Sleep 2s
Ctrl+N
Sleep 1.4s
Ctrl+N
Sleep 1.4s

# Change themes on the fly (52 built in), ending on a dark one.
Type "T"
Sleep 1.5s
Down
Sleep 1s
Down
Sleep 1s
Down
Sleep 1s
Down
Sleep 1s
Enter
Sleep 2.5s
