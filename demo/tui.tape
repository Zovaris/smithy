# The v0.23 TUI demo; demo/run.sh runs it inside a scratch repo with `smithy daemon --demo` up.
Output "@OUT@/tui.gif"
Set Shell "bash"
Set FontSize 15
Set Width 1280
Set Height 760
Set TypingSpeed 40ms
Set Theme "Builtin Dark"
Env COLORTERM "truecolor"

Hide
Type "export PATH=@PATH@ && clear" Enter
Show
Type "smithy" Sleep 300ms Enter
Sleep 3s
Screenshot "@OUT@/tui-firstrun.png"
Escape
Sleep 2s
Type "add a health endpoint with a test" Sleep 300ms Enter
Sleep 4s
Screenshot "@OUT@/tui-tools.png"
Sleep 1s
Type "c"
Enter
Sleep 3s
Screenshot "@OUT@/tui-answered.png"
Sleep 1s
Ctrl+C
Sleep 500ms
