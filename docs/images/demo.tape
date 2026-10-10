# The README demo. Re-record from this folder with `vhs demo.tape`; it needs
# vhs, unsent and claude on PATH. demo-setup.sh makes a throwaway HOME, so no
# real draft or account shows, and nothing is sent anywhere.

Output demo.gif

Set Shell bash
Set FontFamily "Menlo"
Set FontSize 16
Set Width 1200
Set Height 540
Set Padding 24
Set Theme "Catppuccin Mocha"
Set Framerate 12
Set TypingSpeed 45ms
Set CursorBlink false

Hide
Type "source demo-setup.sh && crash_once_saved 'tests green' && clear"
Enter
Sleep 2s
Show

Type "unsent claude"
Sleep 400ms
Enter
Sleep 5s

Type "Make a failed card charge roll back the order and keep the cart,"
Ctrl+J
Type "then return a 402 with the processor's decline code."
Ctrl+J
Type "Add a test for the timeout case and keep the existing tests green."
Sleep 2s

# demo-setup.sh kills claude now, as a crash would.
Hide
Sleep 1.5s
Type "reset && clear"
Enter
Sleep 2s
Show

Type "# claude crashed before you hit Enter. The prompt is still here:"
Sleep 300ms
Enter
Sleep 600ms
Type "unsent list"
Sleep 300ms
Enter
Sleep 2s
Type "unsent show 1"
Sleep 300ms
Enter
Sleep 3s
Type "unsent restore"
Sleep 300ms
Enter
Sleep 4s
Screenshot demo.png
Sleep 1s
