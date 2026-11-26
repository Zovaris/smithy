//go:build harness

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Zovaris/smithy/internal/config"
)

// PairUsage is the text for `smithy pair --help`.
const PairUsage = `usage: smithy pair [list | revoke <name>]
Makes a one-time code (5 minutes, one use) another machine trades for its own device token:
  there: smithy servers add <name> <url>, passing the code with its code flag
The url is this daemon's daemon.listen address, a Tailscale IP (or its tailscale serve URL, which needs no code).
  list     the paired devices
  revoke   forget a device; its token stops working at once`

// PairCmd runs `smithy pair` against the local daemon.
func PairCmd(args []string, out, errw io.Writer) int {
	p := &Pairing{File: DevicesFile()}
	fail := func(err error) int { fmt.Fprintln(errw, "smithy pair:", err); return 1 }
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help":
			fmt.Fprintln(out, PairUsage)
			return 0
		case "list":
			ds, err := p.Devices()
			if err != nil {
				return fail(err)
			}
			if len(ds) == 0 {
				fmt.Fprintln(out, "no paired devices")
			}
			for _, d := range ds {
				fmt.Fprintf(out, "%-20s paired %s\n", d.Name, d.Created.Local().Format("2006-01-02 15:04"))
			}
			return 0
		case "revoke":
			if len(args) != 2 {
				fmt.Fprintln(errw, "smithy pair revoke <name>")
				return 2
			}
			if err := p.Revoke(args[1]); err != nil {
				return fail(err)
			}
			fmt.Fprintln(out, "revoked", args[1])
			return 0
		default:
			fmt.Fprintln(errw, PairUsage)
			return 2
		}
	}
	i := Running()
	if i == nil {
		return fail(fmt.Errorf("no daemon running; start one with smithy daemon --bg"))
	}
	tok, err := LoadToken(TokenFile(), false)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "POST", i.URL()+"/v1/pair/code", bytes.NewReader([]byte("{}")))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return fail(err)
	}
	defer res.Body.Close()
	var b struct {
		Code    string    `json:"code"`
		Expires time.Time `json:"expires"`
		Error   string    `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&b)
	if res.StatusCode != 200 {
		return fail(fmt.Errorf("%s %s", res.Status, b.Error))
	}
	fmt.Fprintf(out, "pairing code: %s  (one use, until %s)\n", b.Code, b.Expires.Local().Format("15:04:05"))
	url := "http://<this machine's address>:<port>"
	if l := config.Get("daemon.listen", ""); l != "" {
		url = "http://" + l
	} else {
		fmt.Fprintln(out, "note: daemon.listen is empty, so other machines cannot reach this daemon directly;\n      smithy config set daemon.listen <tailscale ip>:7478, then smithy stop and smithy")
	}
	fmt.Fprintf(out, "on the other machine: smithy servers add <name> %s --code %s\n", url, b.Code)
	return 0
}
