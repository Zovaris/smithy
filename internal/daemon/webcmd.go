//go:build harness

package daemon

import (
	"fmt"
	"io"
)

// WebUsage is `smithy web --help`.
const WebUsage = `usage: smithy web [open]

Print the running daemon's web UI link (the token rides in the URL fragment, which the browser
never sends to a server); "open" also opens it in the default browser. Behind tailscale serve,
open https://<machine>.<tailnet>.ts.net/ instead: logins listed in daemon.allow need no token.
The link holds the daemon token: "open" passes it to the browser launcher, so other local users
can see it in the process list for a moment.`

// Web runs `smithy web`. running, token and open are seams for tests.
func Web(args []string, running func() *Info, token func() (string, error), open func(string) error, out, errw io.Writer) int {
	doOpen := false
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Fprintln(out, WebUsage)
			return 0
		case "open":
			doOpen = true
		default:
			fmt.Fprintln(errw, "smithy web: unknown argument", a)
			fmt.Fprintln(errw, WebUsage)
			return 2
		}
	}
	i := running()
	if i == nil {
		fmt.Fprintln(errw, "smithy web: no daemon running; start one with: smithy daemon --bg")
		return 1
	}
	tok, err := token()
	if err != nil {
		fmt.Fprintln(errw, "smithy web: token:", err)
		return 1
	}
	u := i.WebURL(tok)
	if doOpen {
		if err := open(u); err != nil {
			fmt.Fprintln(errw, "smithy web: open:", err)
			fmt.Fprintln(out, u)
			return 1
		}
		return 0
	}
	fmt.Fprintln(out, u)
	return 0
}
