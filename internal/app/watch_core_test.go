package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"netpilot/internal/core"
	"netpilot/internal/model"
	"netpilot/internal/store"
)

// fakeProc — ядро в «отдельном процессе», которое тест может уронить.
type fakeProc struct {
	mu      sync.Mutex
	exited  chan struct{}
	starts  int
	failing bool // следующие запуски не удаются
}

func (f *fakeProc) Name() string                    { return "fake" }
func (f *fakeProc) Version() string                 { return "1" }
func (f *fakeProc) Available() bool                 { return true }
func (f *fakeProc) Capabilities() core.Capabilities { return core.Capabilities{} }
func (f *fakeProc) ClashAPI() core.Clash            { return core.Clash{} }
func (f *fakeProc) ExitReason() string              { return "exit status 2: panic" }
func (f *fakeProc) Render(*model.Config, core.Env) ([]byte, error) {
	return nil, nil
}

func (f *fakeProc) Start(*model.Config, core.Env) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing {
		return errors.New("порт занят")
	}
	f.starts++
	f.exited = make(chan struct{})
	return nil
}

func (f *fakeProc) Stop() error {
	f.crash() // остановка тоже завершает процесс
	return nil
}

func (f *fakeProc) crash() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.exited != nil {
		select {
		case <-f.exited:
		default:
			close(f.exited)
		}
	}
}

func (f *fakeProc) Running() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.exited == nil {
		return false
	}
	select {
	case <-f.exited:
		return false
	default:
		return true
	}
}

func (f *fakeProc) Exited() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exited
}

func (f *fakeProc) setFailing(v bool) {
	f.mu.Lock()
	f.failing = v
	f.mu.Unlock()
}

func (f *fakeProc) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts
}

func newWatchApp(t *testing.T) (*App, *fakeProc) {
	t.Helper()
	old := restartDelays
	restartDelays = []time.Duration{20 * time.Millisecond}
	t.Cleanup(func() { restartDelays = old })
	dir := t.TempDir()
	st, err := store.Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Update(func(c *model.Config) error { c.Settings.Core = "fake"; return nil })
	fp := &fakeProc{}
	reg := &core.Registry{}
	reg.Register(fp)
	a := &App{Store: st, Cores: reg, DataDir: dir}
	t.Cleanup(func() { _ = a.Stop() })
	return a, fp
}

func TestCoreCrashRestarts(t *testing.T) {
	a, fp := newWatchApp(t)
	a.mu.Lock()
	if err := a.startLocked(); err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	a.mu.Unlock()

	fp.setFailing(true) // пока ядро «лежит», статус не должен говорить «работает»
	fp.crash()
	waitFor(t, "падение не замечено", func() bool { s := a.Status(); return !s.Running && s.Restarting })
	if s := a.Status(); !strings.Contains(s.Error, "неожиданно завершилось") || !strings.Contains(s.Error, "panic") {
		t.Fatalf("ошибка: %q", s.Error)
	}
	fp.setFailing(false)
	waitFor(t, "ядро не перезапущено", func() bool { return a.Status().Running })
	if s := a.Status(); s.Restarting || s.Error != "" || fp.startCount() != 2 {
		t.Fatalf("после перезапуска: %+v, запусков %d", s, fp.startCount())
	}

	// Остановка — не падение: перезапуска нет.
	if err := a.Stop(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if s := a.Status(); s.Running || s.Restarting || fp.startCount() != 2 {
		t.Fatalf("после остановки: %+v, запусков %d", s, fp.startCount())
	}
}

func TestCoreCrashGivesUp(t *testing.T) {
	a, fp := newWatchApp(t)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	fp.setFailing(true)
	fp.crash()
	waitFor(t, "перезапуск не прекращён", func() bool {
		s := a.Status()
		return !s.Restarting && strings.Contains(s.Error, "перезапуск прекращён")
	})
	if a.Status().Running {
		t.Fatal("статус «работает» у остановленного ядра")
	}
	// Ручной запуск снова работает и сбрасывает счёт сбоев.
	fp.setFailing(false)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	n := len(a.restart.fails)
	a.mu.Unlock()
	if n != 0 || !a.Status().Running {
		t.Fatalf("после ручного запуска: сбоев %d", n)
	}
}

func TestStopCancelsPendingRestart(t *testing.T) {
	a, fp := newWatchApp(t)
	restartDelays = []time.Duration{200 * time.Millisecond}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	fp.crash()
	waitFor(t, "падение не замечено", func() bool { return a.Status().Restarting })
	if err := a.Stop(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if s := a.Status(); s.Running || s.Restarting || fp.startCount() != 1 {
		t.Fatalf("перезапуск после остановки: %+v, запусков %d", s, fp.startCount())
	}
}

// Журнал на момент сбоя остаётся в core.crash.log: перезапуски его не затрут.
func TestKeepCrashLog(t *testing.T) {
	dir := t.TempDir()
	a := &App{DataDir: dir}
	if err := os.WriteFile(filepath.Join(dir, "core.log"), []byte("FATAL: причина сбоя\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.keepCrashLog()
	got, err := os.ReadFile(filepath.Join(dir, "core.crash.log"))
	if err != nil || string(got) != "FATAL: причина сбоя\n" {
		t.Fatalf("core.crash.log: %q %v", got, err)
	}
}
