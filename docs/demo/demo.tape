# TideMail README demo. Record it with docs/demo/record.sh (it builds TideMail, seeds a
# throwaway sandbox of made-up mail, then runs this tape). Output: docs/demo/demo.gif.
#
# Keys used: Tab (next pane), j/k (move), Enter (open), T (theme picker), q (quit).
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

# Open the newsletter: links are listed with their real destination host,
# and tracking pixels are left out.
Down
Down
Sleep 400ms
Enter
Sleep 2s
Tab
Sleep 600ms
Down
Down
Down
Sleep 1.2s

# Change themes on the fly (52 built in).
Type "T"
Sleep 1.5s
Down
Sleep 900ms
Down
Sleep 900ms
Down
Sleep 900ms
Down
Sleep 900ms
Enter
Sleep 2s

# Quit.
Type "q"
Sleep 500ms
Type "y"
Sleep 800ms
