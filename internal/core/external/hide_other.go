//go:build !windows

package external

import "os/exec"

func hideWindow(cmd *exec.Cmd) { setPdeathsig(cmd) }

func bindToParent(*exec.Cmd) {}
