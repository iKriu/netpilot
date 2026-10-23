package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"netpilot/internal/model"
)

// setupConfig создаёт настройки с заданным портом и сохраняет их обычным путём.
func setupConfig(t *testing.T, dir string, port int) {
	t.Helper()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *model.Config) error { c.Settings.MixedPort = port; return nil }); err != nil {
		t.Fatal(err)
	}
}

func writeAt(t *testing.T, path, data string, mod time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func brokenFiles(t *testing.T, dir string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "config.json.broken-*"))
	return m
}

func TestOpenWritesPrev(t *testing.T) {
	dir := t.TempDir()
	setupConfig(t, dir, 7001)
	if _, err := Open(dir, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json.prev"))
	if err != nil {
		t.Fatalf("копия при запуске не сохранена: %v", err)
	}
	if !strings.Contains(string(data), "7001") {
		t.Fatal("в копии не текущие настройки")
	}
}

// Пустой config.json после сбоя питания: берётся .prev (он новее .bak),
// испорченный файл откладывается, восстановленный — записывается на место.
func TestOpenRecoversFromPrev(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	setupConfig(t, dir, 7002)
	if _, err := Open(dir, nil); err != nil { // сохраняет .prev
		t.Fatal(err)
	}
	writeAt(t, path+".bak", `{"settings":{"mixedPort":6000}}`, time.Now().Add(-48*time.Hour))
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir, func(*model.Config) { t.Fatal("firstRun при восстановлении") })
	if err != nil {
		t.Fatalf("не восстановилось: %v", err)
	}
	if got := s.Get().Settings.MixedPort; got != 7002 {
		t.Fatalf("порт %d, ждали 7002 из .prev", got)
	}
	if msg := s.Recovered(); !strings.Contains(msg, "восстановлены из резервной копии") {
		t.Fatalf("нет сообщения о восстановлении: %q", msg)
	}
	if b := brokenFiles(t, dir); len(b) != 1 {
		t.Fatalf("испорченный файл не отложен: %v", b)
	}
	if v := readSchema(t, path); v != model.SchemaVersion {
		t.Fatalf("восстановленный config.json не записан: schema %d", v)
	}
	// Следующий запуск — обычный, без сообщения.
	s2, err := Open(dir, nil)
	if err != nil || s2.Recovered() != "" || s2.Get().Settings.MixedPort != 7002 {
		t.Fatalf("повторный запуск: err=%v msg=%q", err, s2.Recovered())
	}
}

// Только .bak (старого формата, от миграции) — тоже годится.
func TestOpenRecoversFromBak(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeAt(t, path+".bak", `{"settings":{"mixedPort":6001}}`, time.Now().Add(-time.Hour))
	writeAt(t, path+".prev", "", time.Now()) // копия тоже пострадала
	if err := os.WriteFile(path, []byte("\x00\x00\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("не восстановилось: %v", err)
	}
	if got := s.Get().Settings.MixedPort; got != 6001 {
		t.Fatalf("порт %d, ждали 6001 из .bak", got)
	}
	if s.Recovered() == "" {
		t.Fatal("нет сообщения о восстановлении")
	}
	if b := brokenFiles(t, dir); len(b) != 1 {
		t.Fatalf("испорченный файл не отложен: %v", b)
	}
}

// Годных копий нет — отказ запуска, а не настройки по умолчанию поверх
// испорченного файла; сам файл остаётся на месте (иначе следующий запуск
// молча начал бы с чистого листа).
func TestOpenRefusesWithoutBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeAt(t, path+".prev", "{обрыв", time.Now())
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, nil); err == nil {
		t.Fatal("испорченный файл принят без копии")
	}
	if fi, err := os.Stat(path); err != nil || fi.Size() != 0 {
		t.Fatalf("испорченный файл тронут: %v", err)
	}
	if b := brokenFiles(t, dir); len(b) != 0 {
		t.Fatalf("файл отложен, хотя восстановить нечем: %v", b)
	}
}
