// Package auth — вход в веб-интерфейс: пароль (bcrypt), сессии в cookie,
// защита от подбора пароля.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	CookieName  = "np_session"
	SessionTTL  = 30 * 24 * time.Hour // сессия продлевается при каждом обращении
	MinPassword = 8

	maxFailures = 5           // столько ошибок подряд — и адрес блокируется
	baseLockout = time.Minute // первая блокировка; дальше вдвое дольше
	maxLockout  = 30 * time.Minute
)

var (
	ErrNoPassword   = errors.New("пароль ещё не задан")
	ErrWrong        = errors.New("неверный пароль")
	ErrShort        = fmt.Errorf("пароль должен быть не короче %d символов", MinPassword)
	ErrAlreadySetup = errors.New("пароль уже задан")
)

// LockedError — адрес временно заблокирован после серии неудачных попыток.
type LockedError struct{ Until time.Time }

func (e LockedError) Error() string {
	return fmt.Sprintf("слишком много неудачных попыток, повторите через %s", time.Until(e.Until).Round(time.Second))
}

type Auth struct {
	mu       sync.Mutex
	path     string
	state    fileState
	failures map[string]*attempts
}

type fileState struct {
	Hash string `json:"hash"`
	// Хранятся не сами токены, а их SHA-256: утечка файла не даёт войти.
	Sessions map[string]int64 `json:"sessions"` // sha256(токен) → срок действия (unix)
}

type attempts struct {
	count int
	locks int
	until time.Time
}

func Open(dataDir string) (*Auth, error) {
	a := &Auth{path: filepath.Join(dataDir, "auth.json"), failures: map[string]*attempts{}}
	a.state.Sessions = map[string]int64{}
	data, err := os.ReadFile(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &a.state); err != nil {
		return nil, fmt.Errorf("auth.json: %w", err)
	}
	if a.state.Sessions == nil {
		a.state.Sessions = map[string]int64{}
	}
	return a, nil
}

// Configured — задан ли пароль.
func (a *Auth) Configured() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state.Hash != ""
}

// Setup задаёт первый пароль. После этого сменить его можно только зная текущий.
func (a *Auth) Setup(password string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.Hash != "" {
		return "", ErrAlreadySetup
	}
	if err := a.setHashLocked(password); err != nil {
		return "", err
	}
	return a.newSessionLocked()
}

// Login проверяет пароль и возвращает токен новой сессии.
func (a *Auth) Login(client, password string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if at := a.failures[client]; at != nil && time.Now().Before(at.until) {
		return "", LockedError{at.until}
	}
	if a.state.Hash == "" {
		return "", ErrNoPassword
	}
	if bcrypt.CompareHashAndPassword([]byte(a.state.Hash), []byte(password)) != nil {
		a.failLocked(client)
		return "", ErrWrong
	}
	delete(a.failures, client)
	return a.newSessionLocked()
}

func (a *Auth) failLocked(client string) {
	at := a.failures[client]
	if at == nil {
		at = &attempts{}
		a.failures[client] = at
	}
	at.count++
	if at.count >= maxFailures {
		d := baseLockout << at.locks
		if d > maxLockout || d <= 0 {
			d = maxLockout
		}
		at.until, at.count = time.Now().Add(d), 0
		at.locks++
	}
}

// ChangePassword меняет пароль и завершает все остальные сессии.
func (a *Auth) ChangePassword(client, current, next, keepToken string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if at := a.failures[client]; at != nil && time.Now().Before(at.until) {
		return LockedError{at.until}
	}
	if bcrypt.CompareHashAndPassword([]byte(a.state.Hash), []byte(current)) != nil {
		a.failLocked(client)
		return errors.New("текущий пароль указан неверно")
	}
	if err := a.setHashLocked(next); err != nil {
		return err
	}
	keep := hashToken(keepToken)
	for k := range a.state.Sessions {
		if k != keep {
			delete(a.state.Sessions, k)
		}
	}
	return a.saveLocked()
}

// Valid проверяет токен и продлевает сессию.
func (a *Auth) Valid(token string) bool {
	if token == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	h := hashToken(token)
	exp, ok := a.state.Sessions[h]
	if !ok || time.Now().Unix() > exp {
		if ok {
			delete(a.state.Sessions, h)
			_ = a.saveLocked()
		}
		return false
	}
	// Продлеваем не чаще раза в сутки: на роутере лишние записи изнашивают флеш.
	if next := time.Now().Add(SessionTTL).Unix(); next-exp > 86400 {
		a.state.Sessions[h] = next
		_ = a.saveLocked()
	}
	return true
}

func (a *Auth) Logout(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.state.Sessions, hashToken(token))
	_ = a.saveLocked()
}

func (a *Auth) setHashLocked(password string) error {
	if len([]rune(password)) < MinPassword {
		return ErrShort
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	a.state.Hash = string(h)
	return a.saveLocked()
}

func (a *Auth) newSessionLocked() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	now := time.Now().Unix()
	for k, exp := range a.state.Sessions { // заодно чистим истёкшие
		if exp < now {
			delete(a.state.Sessions, k)
		}
	}
	a.state.Sessions[hashToken(token)] = time.Now().Add(SessionTTL).Unix()
	return token, a.saveLocked()
}

func (a *Auth) saveLocked() error {
	data, _ := json.MarshalIndent(a.state, "", "  ")
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.path)
}

func hashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}
