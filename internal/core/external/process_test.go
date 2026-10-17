package external

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Тестовый бинарник сам играет роль ядра: при NETPILOT_FAKE_CORE он
// работает немного дольше проверки запуска и падает.
func TestMain(m *testing.M) {
	switch os.Getenv("NETPILOT_FAKE_CORE") {
	case "fast":
		fmt.Println("listen: порт занят")
		os.Exit(1)
	case "1":
		time.Sleep(1500 * time.Millisecond)
		fmt.Println("fatal: что-то сломалось")
		os.Exit(3)
	}
	os.Exit(m.Run())
}

func TestProcessExitObserved(t *testing.T) {
	t.Setenv("NETPILOT_FAKE_CORE", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var p Process
	if err := p.Start(exe, nil, dir, filepath.Join(dir, "core.log")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Exited():
	case <-time.After(10 * time.Second):
		t.Fatal("завершение процесса не замечено")
	}
	if p.Running() {
		t.Fatal("Running после завершения")
	}
	if r := p.ExitReason(); !strings.Contains(r, "3") || !strings.Contains(r, "что-то сломалось") {
		t.Fatalf("причина: %q", r)
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Exited(): // не запущен — канал закрыт, наблюдатель не повиснет
	default:
		t.Fatal("Exited не закрыт у остановленного процесса")
	}
}

// Падение сразу после запуска — ошибка запуска, а не «работает».
func TestProcessEarlyExit(t *testing.T) {
	t.Setenv("NETPILOT_FAKE_CORE", "fast")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var p Process
	err = p.Start(exe, nil, dir, filepath.Join(dir, "core.log"))
	if err == nil || !strings.Contains(err.Error(), "порт занят") {
		t.Fatalf("ждали ошибку запуска, получили %v", err)
	}
	if p.Running() {
		t.Fatal("Running у упавшего процесса")
	}
}
