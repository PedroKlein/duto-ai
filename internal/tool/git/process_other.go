//go:build !darwin && !linux

package git

import (
	"os"
	"os/exec"
)

func configureProcessGroup(*exec.Cmd) {}

func killProcessGroup(process *os.Process) error {
	if process == nil {
		return os.ErrProcessDone
	}

	return process.Kill()
}
