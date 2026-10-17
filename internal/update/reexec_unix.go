//go:build !windows

package update

import (
	"log"
	"os"
	"syscall"
)

// reexec заменяет текущий процесс новой программой с теми же аргументами.
// PID сохраняется, поэтому systemd и procd не замечают перезапуска.
func reexec(exe string) {
	if err := syscall.Exec(exe, append([]string{exe}, os.Args[1:]...), os.Environ()); err != nil {
		log.Printf("перезапуск: %v — завершаюсь, служба запустит программу заново", err)
		os.Exit(1)
	}
}
