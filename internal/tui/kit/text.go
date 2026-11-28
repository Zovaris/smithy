package kit

import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Version is the binary's version, shown in the header.
var Version = "dev"

var c1ToESC = strings.NewReplacer("\u009b", "\x1b[", "\u009d", "\x1b]", "\u0090", "\x1bP", "\u009e", "\x1b^", "\u009f", "\x1b_", "\u0098", "\x1bX")

// clean drops escape sequences and C0/C1 control characters, so issue and card text can't drive the terminal.
func Clean(s string) string {
	s = strings.Map(func(r rune) rune { // C1 controls as runes (U+009B is CSI) become ESC forms Strip knows
		if r >= 0x80 && r <= 0x9f {
			return -1
		}
		return r
	}, c1ToESC.Replace(s))
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			return ' '
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			return -1
		}
		return r
	}, s)
}

func Short(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// OpenURL opens u in the default browser.
func OpenURL(u string) error {
	name := "xdg-open"
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	}
	return exec.Command(name, u).Start()
}
