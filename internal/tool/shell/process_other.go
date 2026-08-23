//go:build !darwin && !linux

package shell

import (
	"fmt"
	"os"
	"os/exec"
)

func configureProcessGroup(*exec.Cmd) {}

func killProcessGroup(process *os.Process) error {
	if process == nil {
		return os.ErrProcessDone
	}

	if err := process.Kill(); err != nil {
		return fmt.Errorf("killing trusted process: %w", err)
	}

	return nil
}
