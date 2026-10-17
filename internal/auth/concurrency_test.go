package auth

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Пока Login проверяет пароль (bcrypt), остальные запросы не должны ждать:
// подменяем проверку на «зависшую» и убеждаемся, что Valid проходит.
func TestLoginDoesNotHoldLockDuringBcrypt(t *testing.T) {
	a, _ := Open(t.TempDir())
	tok, err := a.Setup("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	orig := compareHash
	compareHash = func(h, p []byte) error {
		close(entered)
		<-release
		return orig(h, p)
	}
	defer func() { compareHash = orig }()

	done := make(chan error, 1)
	go func() {
		_, err := a.Login("10.0.0.2", "correct horse")
		done <- err
	}()
	<-entered // Login сейчас внутри проверки пароля

	valid := make(chan bool, 1)
	go func() { valid <- a.Valid(tok) }()
	select {
	case ok := <-valid:
		if !ok {
			t.Fatal("сессия должна быть действительной")
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("Valid ждёт, пока Login проверяет пароль")
	}
	// Вторая попытка с того же адреса, пока идёт первая, отклоняется.
	if _, err := a.Login("10.0.0.2", "x"); !errors.Is(err, ErrBusy) {
		t.Fatalf("параллельная попытка с того же адреса: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("login: %v", err)
	}
}

func TestFailuresBounded(t *testing.T) {
	a, _ := Open(t.TempDir())
	a.mu.Lock()
	for i := 0; i < maxTracked*2; i++ {
		a.failLocked(fmt.Sprintf("10.%d.%d.1", i/256, i%256))
	}
	n := len(a.failures)
	// Давняя запись без блокировки забывается при очередной очистке.
	a.failures["old"] = &attempts{count: 1, last: time.Now().Add(-2 * forgetAfter)}
	a.pruneLocked(time.Now())
	_, oldKept := a.failures["old"]
	a.mu.Unlock()
	if n > maxTracked {
		t.Fatalf("таблица ошибок растёт без предела: %d", n)
	}
	if oldKept {
		t.Fatal("давняя запись не забыта")
	}
}

func TestSetupCode(t *testing.T) {
	a, _ := Open(t.TempDir())
	code, err := a.NewSetupCode()
	if err != nil || len(code) != 14 || strings.Count(code, "-") != 2 {
		t.Fatalf("код %q: %v", code, err)
	}
	if _, err := a.SetupWithCode("10.0.0.3", "AAAA-AAAA-AAAA", "correct horse"); !errors.Is(err, ErrSetupCode) {
		t.Fatalf("неверный код: %v", err)
	}
	// Регистр и дефисы не важны.
	tok, err := a.SetupWithCode("10.0.0.3", strings.ToLower(strings.ReplaceAll(code, "-", "")), "correct horse")
	if err != nil || !a.Valid(tok) {
		t.Fatalf("верный код: %v", err)
	}
	if again, _ := a.NewSetupCode(); again != "" {
		t.Fatal("после настройки код не нужен")
	}
	if _, err := a.SetupWithCode("10.0.0.3", code, "other password"); err == nil {
		t.Fatal("код одноразовый")
	}
}
