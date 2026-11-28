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
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/charmbracelet/x/ansi"
)

const (
	shellMaxLines = 200
	shellFold     = 20
	clipMaxBytes  = 32 << 20
)

var errNoImage = errors.New("no image in the clipboard")

// shellBlock is a `!cmd` in the transcript: run is nil while it runs.
type shellBlock struct {
	cmd, dir string
	run      *daemon.ShellRun
}

type shellDoneMsg struct {
	c   *chatMsg
	run daemon.ShellRun
}

type clipMsg struct {
	data []byte
	err  error
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
	m.offset = 0
	m.push(chatMsg{who: "daemon", kind: "shell", sh: &shellBlock{cmd: line, dir: m.dir}})
	c := m.msgs[len(m.msgs)-1]
	run, dir := m.shell, m.dir
	if run == nil {
		run = daemon.RunShell
	}
	return m, func() tea.Msg {
		return shellDoneMsg{c: c, run: run(context.Background(), dir, line)}
	}
}

func (m *chatView) shellDone(msg shellDoneMsg) {
	r := msg.run
	if len(r.Lines) > shellMaxLines {
		r.Lines = append([]daemon.ShellLine{{Text: fmt.Sprintf("… %d lines cut", len(r.Lines)-shellMaxLines)}}, r.Lines[len(r.Lines)-shellMaxLines:]...)
	}
	for i, l := range r.Lines {
		r.Lines[i].Text = strings.TrimRight(cleanOutput(l.Text), "\r")
	}
	if r.Cut {
		r.Lines = append(r.Lines, daemon.ShellLine{Text: "… output cut at 256 KiB"})
	}
	msg.c.sh.run, msg.c.lines = &r, nil
	*m.at = anchor{}
	m.offset = 0
}

// shellRows is a `!cmd` block: the command, its status, time and folder (when not the
// session's) on one row; the output under a rule, muted, stderr tinted, folded after shellFold
// lines until a click or enter opens it.
func (m chatView) shellRows(i, w int) []string {
	t := m.th
	c := m.msgs[i]
	sh := c.sh
	p := strings.Repeat(" ", m.pad())
	dim := t.fg(t.dim)
	left := p + "  " + t.fg(t.accent).Render("$") + " " + t.fg(t.strong).Bold(true).Render(sh.cmd)
	var meta []string
	if s, ok := m.session(); ok && s.dir != "" && sh.dir != "" && s.dir != sh.dir {
		meta = append(meta, dim.Render(sh.dir))
	}
	switch r := sh.run; {
	case r == nil:
		meta = append(meta, t.fg(t.warn).Render("running"))
	default:
		meta = append(meta, dim.Render(shellTook(r.Took)))
		switch {
		case r.Err != "":
			meta = append(meta, t.fg(t.err).Render(r.Err))
		case r.Code != 0:
			meta = append(meta, t.fg(t.err).Render(fmt.Sprintf("exit %d", r.Code)))
		default:
			meta = append(meta, t.fg(t.ok).Render("ok"))
		}
	}
	right := strings.Join(meta, dim.Render(" "+t.g.dot+" "))
	head := spread(ansi.Truncate(left, max(w-ansi.StringWidth(right)-m.pad()-4, 10), "…"), right+p+"  ", w)
	rows := []string{m.blockRow(i, w, head)}
	if sh.run == nil {
		return append(rows, "")
	}
	lines := sh.run.Lines
	if len(lines) == 0 {
		lines = []daemon.ShellLine{{Text: "(no output)"}}
	}
	more := 0
	if !c.open && len(lines) > shellFold+2 {
		more, lines = len(lines)-shellFold, lines[:shellFold]
	}
	rule := p + "  " + dim.Render(t.g.pipe) + " "
	for _, l := range lines {
		st := t.fg(t.muted)
		if l.Err {
			st = t.fg(t.err)
		}
		for _, wl := range strings.Split(ansi.Wrap(strings.ReplaceAll(l.Text, "\t", "    "), max(w-2*m.pad()-6, 10), ""), "\n") {
			rows = append(rows, m.blockRow(i, w, rule+st.Render(wl)))
		}
	}
	if more > 0 {
		rows = append(rows, m.blockRow(i, w, rule+dim.Render(fmt.Sprintf("… %d more lines · click or enter to open", more))))
	}
	return append(rows, "")
}

// shellTook is a run's time: 40ms, 1.2s, 2m3s.
func shellTook(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return d.Round(time.Second).String()
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
