package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriterRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.log")
	w, err := Create(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"aaaa\n", "bbbb\n", "cccc\n"} {
		if _, err := w.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	cur, _ := os.ReadFile(path)
	old, _ := os.ReadFile(path + ".1")
	if string(cur) != "cccc\n" || string(old) != "aaaa\nbbbb\n" {
		t.Fatalf("журнал %q, старый %q", cur, old)
	}
}

func TestTrim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString(strings.Repeat("x", 20))
	if err := Trim(path, 10); err != nil {
		t.Fatal(err)
	}
	f.WriteString("new\n") // пишущий продолжает с начала обрезанного файла
	cur, _ := os.ReadFile(path)
	old, _ := os.ReadFile(path + ".1")
	if string(cur) != "new\n" || len(old) != 20 {
		t.Fatalf("журнал %q, старый %d байт", cur, len(old))
	}
}

// Новый запуск не стирает журнал прошлого: он остаётся в .1.
func TestCreateKeepsPrevious(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.log")
	if err := os.WriteFile(path, []byte("прошлый запуск\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := Create(path, MaxSize)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("новый\n"))
	w.Close()
	prev, _ := os.ReadFile(path + ".1")
	cur, _ := os.ReadFile(path)
	if string(prev) != "прошлый запуск\n" || string(cur) != "новый\n" {
		t.Fatalf(".1=%q текущий=%q", prev, cur)
	}
	// Пустой журнал (ядро упало, ничего не написав) не затирает сохранённый.
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	KeepPrevious(path)
	if prev2, _ := os.ReadFile(path + ".1"); string(prev2) != "прошлый запуск\n" {
		t.Fatalf("пустой журнал затёр сохранённый: %q", prev2)
	}
}
