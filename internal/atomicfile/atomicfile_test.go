package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteReplacesAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := Write(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "new" {
		t.Fatalf("содержимое %q, ждали new", b)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("временный файл остался: %v", err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Fatalf("права %v, ждали 0600", fi.Mode().Perm())
		}
	}
}

// Остаток прошлой оборванной записи не мешает и не передаёт свои права.
func TestWriteStaleTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path+".tmp", []byte("мусор от сбоя"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "{}" {
		t.Fatalf("содержимое %q", b)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Fatalf("права %v, ждали 0600", fi.Mode().Perm())
		}
	}
}

// Если записать не удалось, прежний файл цел и новое содержимое не появилось.
func TestWriteFailureKeepsOld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := Write(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Непустой каталог на месте временного файла: Remove его не удалит,
	// и создать временный файл не получится.
	if err := os.MkdirAll(filepath.Join(path+".tmp", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new"), 0o600); err == nil {
		t.Fatal("ждали ошибку")
	}
	if b, _ := os.ReadFile(path); string(b) != "old" {
		t.Fatalf("прежний файл испорчен: %q", b)
	}
}
