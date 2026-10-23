package auth

import (
	"os"
	"path/filepath"
	"testing"
)

// После сбоя питания auth.json пуст: состояние поднимается из копии,
// сделанной при прошлом запуске, и пароль и сессии продолжают действовать.
func TestOpenRestoresFromPrev(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := a.Setup("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	// Следующий запуск сохраняет auth.json.prev.
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "auth.json")
	if _, err := os.Stat(path + ".prev"); err != nil {
		t.Fatalf("копия не сохранена: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Open(dir)
	if err != nil {
		t.Fatalf("не восстановилось: %v", err)
	}
	if !b.Configured() {
		t.Fatal("после восстановления пароль считается не заданным")
	}
	if !b.Valid(tok) {
		t.Fatal("сессия из копии не действует")
	}
	if _, err := b.Login("1.2.3.4", "correct horse"); err != nil {
		t.Fatalf("прежний пароль не подходит: %v", err)
	}
	broken, _ := filepath.Glob(path + ".broken-*")
	if len(broken) != 1 {
		t.Fatalf("испорченный файл не отложен: %v", broken)
	}
	// Восстановленный файл записан и читается следующим запуском.
	if c, err := Open(dir); err != nil || !c.Configured() {
		t.Fatalf("повторный запуск: %v", err)
	}
}

// Без годной копии запуск отказывает, а не открывает первую настройку пароля
// кому угодно; испорченный файл остаётся на месте.
func TestOpenCorruptWithoutPrevFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte("\x00\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("испорченный auth.json принят")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("испорченный файл убран, следующий запуск открыл бы настройку пароля: %v", err)
	}
	// Копия без пароля тоже не годится.
	if err := os.WriteFile(path+".prev", []byte(`{"hash":"","sessions":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("копия без пароля принята")
	}
}
