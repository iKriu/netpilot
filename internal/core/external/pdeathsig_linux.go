package external

import (
	"os/exec"
	"syscall"
)

// Если NetPilot умрёт, ядро получит SIGKILL и не останется занимать порты.
func setPdeathsig(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
