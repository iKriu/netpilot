package auth

import (
	"errors"
	"testing"
)

func TestFlow(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.Configured() {
		t.Fatal("новая установка не должна иметь пароля")
	}
	if _, err := a.Setup("short"); !errors.Is(err, ErrShort) {
		t.Fatalf("короткий пароль: %v", err)
	}
	tok, err := a.Setup("correct horse")
	if err != nil || !a.Valid(tok) {
		t.Fatalf("setup: %v", err)
	}
	if _, err := a.Setup("another pass"); !errors.Is(err, ErrAlreadySetup) {
		t.Fatal("повторная настройка должна быть запрещена")
	}

	// Сессия переживает перезапуск программы.
	b, _ := Open(dir)
	if !b.Valid(tok) || b.Valid("forged") {
		t.Fatal("сессии после перезапуска")
	}

	// Смена пароля завершает остальные сессии, текущую оставляет.
	other, _ := b.Login("10.0.0.1", "correct horse")
	if err := b.ChangePassword("10.0.0.1", "correct horse", "new password 1", tok); err != nil {
		t.Fatal(err)
	}
	if !b.Valid(tok) || b.Valid(other) {
		t.Fatal("после смены пароля должна остаться только текущая сессия")
	}
	if _, err := b.Login("10.0.0.1", "correct horse"); err == nil {
		t.Fatal("старый пароль не должен подходить")
	}
	b.Logout(tok)
	if b.Valid(tok) {
		t.Fatal("выход не завершил сессию")
	}
}

func TestLockout(t *testing.T) {
	a, _ := Open(t.TempDir())
	_, _ = a.Setup("correct horse")
	for i := 0; i < maxFailures; i++ {
		if _, err := a.Login("1.2.3.4", "wrong"); !errors.Is(err, ErrWrong) {
			t.Fatalf("попытка %d: %v", i, err)
		}
	}
	// Теперь даже верный пароль не принимается, пока не истечёт блокировка.
	var locked LockedError
	if _, err := a.Login("1.2.3.4", "correct horse"); !errors.As(err, &locked) {
		t.Fatalf("ожидалась блокировка, получено %v", err)
	}
	// Другой адрес не затронут.
	if _, err := a.Login("5.6.7.8", "correct horse"); err != nil {
		t.Fatalf("другой адрес: %v", err)
	}
}
