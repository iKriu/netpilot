//go:build !linux && !windows

package external

import "os/exec"

func setPdeathsig(*exec.Cmd) {}
