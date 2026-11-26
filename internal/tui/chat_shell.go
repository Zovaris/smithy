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
)

const (
	shellTimeout  = 2 * time.Minute
	shellMaxLines = 200
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
	out, err := cmd.CombinedOutput()
	return string(out), err
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
	out := strings.TrimRight(msg.out, "\n")
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
	dir := m.pasteDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "smithy-paste")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		m.status("paste failed: " + err.Error())
		return
	}
	p := filepath.Join(dir, fmt.Sprintf("paste-%d-%d.png", time.Now().UnixNano(), len(m.images)+1))
	if err := os.WriteFile(p, msg.data, 0o600); err != nil {
		m.status("paste failed: " + err.Error())
		return
	}
	m.images = append(m.images, p)
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
