package update

import (
	"log"
	"os"
	"os/exec"
)

// reexec запускает новую программу отдельным процессом и завершает текущий.
func reexec(exe string) {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("перезапуск: %v", err)
	}
	os.Exit(0)
}
