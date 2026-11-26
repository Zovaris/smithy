//go:build harness

package tui

import (
	"context"
	hexenc "encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	shellTimeout  = 2 * time.Minute
	shellMaxLines = 200
	shellMaxBytes = 256 << 10
	clipMaxBytes  = 32 << 20
)

var errNoImage = errors.New("no image in the clipboard")

type shellDoneMsg struct {
	line, out string
	err       error
}

type clipMsg struct {
	data []byte
	err  error
}

// runShell runs line with the user's shell in dir and returns stdout and stderr together.
func runShell(ctx context.Context, dir, line string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, sh, "-c", line)
	cmd.Dir = dir
	cmd.Stdin = nil
	var out capped
	out.max = shellMaxBytes
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if out.cut {
		return string(out.b) + "\n… output cut at 256 KiB", err
	}
	return string(out.b), err
}

// capped keeps the first max bytes written and drops the rest.
type capped struct {
	b   []byte
	max int
	cut bool
}

func (c *capped) Write(p []byte) (int, error) {
	if n := c.max - len(c.b); n < len(p) {
		c.b, c.cut = append(c.b, p[:max(n, 0)]...), true
		return len(p), nil
	}
	c.b = append(c.b, p...)
	return len(p), nil
}

// cleanOutput drops terminal escape sequences and control characters except newline and tab.
func cleanOutput(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || (r >= 0x20 && r != 0x7f && (r < 0x80 || r > 0x9f)) {
			return r
		}
		return -1
	}, s)
}

// bang is enter on a `!cmd` line: it runs in the session folder and never reaches the agent.
func (m chatView) bang(line string) (chatView, tea.Cmd) {
	if line == "" {
		return m, nil
	}
	m.system("$ " + line)
	run, dir := m.shell, m.dir
	if run == nil {
		run = runShell
	}
	return m, func() tea.Msg {
		out, err := run(context.Background(), dir, line)
		return shellDoneMsg{line: line, out: out, err: err}
	}
}

func (m *chatView) shellDone(msg shellDoneMsg) {
	out := strings.TrimRight(cleanOutput(msg.out), "\n")
	if ls := strings.Split(out, "\n"); len(ls) > shellMaxLines {
		out = fmt.Sprintf("… %d lines cut\n", len(ls)-shellMaxLines) + strings.Join(ls[len(ls)-shellMaxLines:], "\n")
	}
	if msg.err != nil {
		out = strings.TrimLeft(out+"\n"+m.th.g.fail+" "+msg.err.Error(), "\n")
	}
	if out == "" {
		out = "(no output)"
	}
	m.system(out)
}

// pasteImage is ctrl+v: it reads an image from the clipboard.
func (m chatView) pasteImage() tea.Cmd {
	read := m.clip
	if read == nil {
		read = readClipImage
	}
	return func() tea.Msg {
		b, err := read()
		return clipMsg{data: b, err: err}
	}
}

func (m *chatView) clipped(msg clipMsg) {
	if msg.err != nil || len(msg.data) == 0 {
		if msg.err == nil || errors.Is(msg.err, errNoImage) {
			m.status("no image in the clipboard")
		} else {
			m.status("paste failed: " + msg.err.Error())
		}
		return
	}
	if len(msg.data) > clipMaxBytes {
		m.status("paste failed: image over 32 MiB")
		return
	}
	if m.pasteDir == "" {
		d, err := os.MkdirTemp("", "smithy-paste-")
		if err != nil {
			m.status("paste failed: " + err.Error())
			return
		}
		m.pasteDir = d
	}
	p := filepath.Join(m.pasteDir, fmt.Sprintf("paste-%d-%d.png", time.Now().UnixNano(), len(m.images)+1))
	if err := writeNew(p, msg.data); err != nil {
		m.status("paste failed: " + err.Error())
		return
	}
	m.images = append(m.images, p)
}

func writeNew(p string, b []byte) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// withImages appends the attached images' paths to text so the agent reads them.
func withImages(text string, imgs []string) string {
	if len(imgs) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(text)
	b.WriteString("\n\nAttached images:")
	for _, p := range imgs {
		b.WriteString("\n" + p)
	}
	return b.String()
}

func (m chatView) chips() string {
	var c []string
	for i := range m.images {
		c = append(c, fmt.Sprintf("[image %d]", i+1))
	}
	return strings.Join(c, " ")
}

// readClipImage reads a PNG from the system clipboard.
func readClipImage() ([]byte, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("osascript", "-e", "the clipboard as «class PNGf»").Output()
		if err != nil {
			return nil, errNoImage
		}
		return parseOsaPNG(string(out))
	case "windows":
		return nil, errors.New("image paste is not supported on windows yet")
	}
	for _, c := range [][]string{{"wl-paste", "--type", "image/png"}, {"xclip", "-selection", "clipboard", "-t", "image/png", "-o"}} {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		if out, err := exec.Command(c[0], c[1:]...).Output(); err == nil && len(out) > 0 {
			return out, nil
		}
		return nil, errNoImage
	}
	return nil, errors.New("image paste needs wl-paste or xclip")
}

// parseOsaPNG decodes osascript's «data PNGf<hex>» form.
func parseOsaPNG(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "«data PNGf")
	s = strings.TrimSuffix(s, "»")
	if s == "" {
		return nil, errNoImage
	}
	return hexenc.DecodeString(s)
}
