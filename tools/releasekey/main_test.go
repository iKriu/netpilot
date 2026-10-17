package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateSignVerify(t *testing.T) {
	dir := t.TempDir()
	if _, err := generate(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := generate(dir); err == nil {
		t.Fatal("повторное создание должно отказывать, а не перезаписывать ключ")
	}
	file := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(file, []byte("abc  netpilot-linux-amd64\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := signFile(filepath.Join(dir, "release.key"), file); err != nil {
		t.Fatal(err)
	}
	pub := filepath.Join(dir, "release.pub")
	if err := verifyFile(pub, file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("abd  netpilot-linux-amd64\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyFile(pub, file); err == nil {
		t.Fatal("изменённый файл прошёл проверку")
	}
	// Чужой ключ.
	other := t.TempDir()
	if _, err := generate(other); err != nil {
		t.Fatal(err)
	}
	if err := signFile(filepath.Join(other, "release.key"), file); err != nil {
		t.Fatal(err)
	}
	if err := verifyFile(pub, file); err == nil {
		t.Fatal("подпись чужим ключом прошла проверку")
	}
}
