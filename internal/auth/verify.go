package auth

import (
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Verify проверяет пароль без создания сессии — для подтверждения опасных
// действий (например, смены источника обновлений). Подчиняется той же
// блокировке после серии ошибок, что и вход: иначе это был бы обходной путь
// для подбора пароля.
func (a *Auth) Verify(client, password string) error {
	a.mu.Lock()
	if at := a.failures[client]; at != nil && time.Now().Before(at.until) {
		a.mu.Unlock()
		return LockedError{at.until}
	}
	hash := a.state.Hash
	a.mu.Unlock()
	if hash == "" {
		return ErrNoPassword
	}
	// bcrypt медленный намеренно — держать на нём мьютекс значит остановить
	// все запросы, проверяющие сессию.
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.failLocked(client)
		return ErrWrong
	}
	delete(a.failures, client)
	return nil
}
