package proof

import (
	"os"
	"os/exec"
)

func ownGroup(cmd *exec.Cmd) {}

func forward(cmd *exec.Cmd, s os.Signal) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
