package agents

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Zovaris/smithy/internal/agents/skills"
	"github.com/Zovaris/smithy/internal/layout"
)

const pathMarker = layout.PathMarker

func binDir() string { return layout.BinDir() }

// RCFile is the shell startup file `setup path add` writes to, from $SHELL.
func RCFile() string {
	home := layout.UserHome()
	switch filepath.Base(os.Getenv("SHELL")) {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish")
	case "bash":
		if _, err := os.Stat(filepath.Join(home, ".bash_profile")); err == nil {
			return filepath.Join(home, ".bash_profile")
		}
		return filepath.Join(home, ".bashrc")
	}
	return filepath.Join(home, ".profile")
}

func pathLine(rc string) string {
	if strings.HasSuffix(rc, "config.fish") {
		return "fish_add_path " + binDir()
	}
	return `export PATH="` + binDir() + `:$PATH"`
}

func onPath() string {
	p, err := exec.LookPath("smithy")
	if err != nil {
		return ""
	}
	return p
}

func pathCmd(args []string) int {
	rc := RCFile()
	b, _ := os.ReadFile(rc)
	present := strings.Contains(string(b), pathMarker)
	if len(args) == 0 || args[0] == "status" {
		switch {
		case onPath() != "":
			fmt.Println("path: smithy is on PATH (" + onPath() + ")")
		case present:
			fmt.Println("path: added in " + rc + "; open a new terminal")
		default:
			fmt.Printf("path: smithy is not on PATH (smithy setup path add writes `%s` to %s)\n", pathLine(rc), rc)
		}
		return 0
	}
	if args[0] != "add" {
		fmt.Fprintln(os.Stderr, "usage: smithy setup path [status|add]")
		return 2
	}
	if present {
		fmt.Println("already in " + rc)
		return 0
	}
	if err := os.MkdirAll(filepath.Dir(rc), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer f.Close()
	sep := "\n"
	if len(b) == 0 || strings.HasSuffix(string(b), "\n") {
		sep = ""
	}
	if _, err := fmt.Fprintf(f, "%s\n%s\n%s\n", sep, pathMarker, pathLine(rc)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	RecordPathFile(rc)
	fmt.Printf("added %s to %s; open a new terminal, then run `smithy status`\n", binDir(), rc)
	return 0
}

// RecordPathFile notes rc in layout.PathRecord, so uninstall removes the PATH line it got.
func RecordPathFile(rc string) { skills.RecordLine(layout.PathRecord(), rc) }
