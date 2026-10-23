// Package auth — вход в веб-интерфейс: пароль (bcrypt), сессии в cookie,
// защита от подбора пароля.
package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"netpilot/internal/atomicfile"
)

const (
	CookieName  = "np_session"
	SessionTTL  = 30 * 24 * time.Hour // сессия продлевается при каждом обращении
	MinPassword = 8

	maxFailures = 5           // столько ошибок подряд — и адрес блокируется
	baseLockout = time.Minute // первая блокировка; дальше вдвое дольше
	maxLockout  = 30 * time.Minute

	// Сколько адресов помним одновременно: иначе перебор со множества адресов
	// раздувал бы таблицу без предела.
	maxTracked = 1024
	// Через столько после последней ошибки (и конца блокировки) адрес забываем.
	forgetAfter = maxLockout
)

var (
	ErrNoPassword   = errors.New("пароль ещё не задан")
	ErrWrong        = errors.New("неверный пароль")
	ErrShort        = fmt.Errorf("пароль должен быть не короче %d символов", MinPassword)
	ErrAlreadySetup = errors.New("пароль уже задан")
	ErrSetupCode    = errors.New("неверный код первой настройки")
	ErrBusy         = errors.New("предыдущая попытка ещё проверяется, повторите чуть позже")
)

// compareHash — переменная, чтобы тест мог задержать проверку пароля
// и убедиться, что она идёт без глобальной блокировки.
var compareHash = bcrypt.CompareHashAndPassword

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
	// Адреса, чей пароль сейчас проверяется (bcrypt идёт без a.mu). Одна
	// проверка на адрес: иначе параллельные запросы успели бы перебрать
	// много паролей до того, как сработает блокировка.
	checking map[string]bool
	// Одноразовый код первой настройки для входа не с этого компьютера,
	// нормализованный (без дефисов, в верхнем регистре). Только в памяти.
	setupCode string
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
	last  time.Time // последняя ошибка — по ней забываем давние записи
}

func Open(dataDir string) (*Auth, error) {
	a := &Auth{path: filepath.Join(dataDir, "auth.json"), failures: map[string]*attempts{}, checking: map[string]bool{}}
	a.state.Sessions = map[string]int64{}
	data, err := os.ReadFile(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &a.state); err != nil {
		if !corrupt(data) {
			return nil, fmt.Errorf("auth.json: %w", err)
		}
		// Испорченный auth.json НЕЛЬЗЯ считать «пароль не задан»: тогда первый,
		// кто откроет панель после сбоя питания, задал бы свой пароль и получил
		// управление шлюзом. Поэтому только восстановление из копии, а без неё —
		// отказ запуска (лучше без панели, чем с чужой).
		if err := a.restorePrev(data, err); err != nil {
			return nil, err
		}
	}
	if a.state.Sessions == nil {
		a.state.Sessions = map[string]int64{}
	}
	// Копия на случай, если auth.json испортится при следующем отключении
	// питания. Раз за запуск, а не при каждом входе: меньше износ флеш-памяти;
	// при восстановлении теряются только сессии новее копии (и пароль, если
	// его сменили после запуска).
	if a.state.Hash != "" {
		if b, err := json.MarshalIndent(a.state, "", "  "); err == nil {
			if err := atomicfile.Write(a.path+".prev", b, 0o600); err != nil {
				log.Printf("не удалось сохранить резервную копию настроек входа: %v", err)
			}
		}
	}
	return a, nil
}

// corrupt — файл испорчен на уровне диска: пустой или вовсе не JSON.
func corrupt(data []byte) bool {
	return len(bytes.TrimSpace(data)) == 0 || !json.Valid(data)
}

// restorePrev поднимает состояние из auth.json.prev. Испорченный файл
// откладывается в auth.json.broken-<время> — только после удачного
// восстановления, иначе следующий запуск не нашёл бы auth.json и открыл бы
// первую настройку пароля кому угодно.
func (a *Auth) restorePrev(data []byte, loadErr error) error {
	log.Printf("ВНИМАНИЕ: %s повреждён (%d байт, %v) — возможно, было внезапное отключение питания; пробую резервную копию", a.path, len(data), loadErr)
	fail := func(why string) error {
		return fmt.Errorf("auth.json повреждён (%v), а %s; восстановите файл вручную или удалите его, чтобы задать пароль заново", loadErr, why)
	}
	b, err := os.ReadFile(a.path + ".prev")
	if err != nil {
		return fail("резервной копии auth.json.prev нет")
	}
	var st fileState
	if err := json.Unmarshal(b, &st); err != nil || st.Hash == "" {
		return fail("резервная копия auth.json.prev негодна")
	}
	broken := fmt.Sprintf("%s.broken-%d", a.path, time.Now().Unix())
	if err := os.Rename(a.path, broken); err != nil {
		return fail("отложить его не удалось: " + err.Error())
	}
	atomicfile.SyncDir(filepath.Dir(a.path))
	a.state = st
	if a.state.Sessions == nil {
		a.state.Sessions = map[string]int64{}
	}
	if err := a.saveLocked(); err != nil {
		return err
	}
	log.Printf("ВНИМАНИЕ: настройки входа восстановлены из auth.json.prev; повреждённый файл сохранён как %s", filepath.Base(broken))
	return nil
}

// Configured — задан ли пароль.
func (a *Auth) Configured() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state.Hash != ""
}

// Setup задаёт первый пароль. После этого сменить его можно только зная текущий.
func (a *Auth) Setup(password string) (string, error) {
	if a.Configured() {
		return "", ErrAlreadySetup
	}
	// bcrypt — вне блокировки: на слабом роутере это около секунды.
	h, err := hashPassword(password)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.Hash != "" {
		return "", ErrAlreadySetup
	}
	a.state.Hash = h
	if err := a.saveLocked(); err != nil {
		a.state.Hash = ""
		return "", err
	}
	a.setupCode = "" // код одноразовый
	return a.newSessionLocked()
}

// SetupWithCode — первая настройка не с этого компьютера: нужен код из журнала.
// Иначе любой в сети успел бы задать свой пароль раньше владельца.
func (a *Auth) SetupWithCode(client, code, password string) (string, error) {
	a.mu.Lock()
	if at := a.failures[client]; at != nil && time.Now().Before(at.until) {
		a.mu.Unlock()
		return "", LockedError{at.until}
	}
	if a.state.Hash != "" {
		a.mu.Unlock()
		return "", ErrAlreadySetup
	}
	want := a.setupCode
	if want == "" || subtle.ConstantTimeCompare([]byte(normalizeCode(code)), []byte(want)) != 1 {
		a.failLocked(client)
		a.mu.Unlock()
		return "", ErrSetupCode
	}
	a.mu.Unlock()
	return a.Setup(password)
}

// NewSetupCode создаёт одноразовый код первой настройки, если пароль ещё не задан.
// Возвращает код в удобном для ввода виде (XXXX-XXXX-XXXX) или "", если пароль уже есть.
func (a *Auth) NewSetupCode() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.Hash != "" {
		return "", nil
	}
	code, err := randomCode(12)
	if err != nil {
		return "", err
	}
	a.setupCode = code
	return code[0:4] + "-" + code[4:8] + "-" + code[8:12], nil
}

// Без похожих символов (0/O, 1/I/L): код переписывают из журнала руками.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func randomCode(n int) (string, error) {
	out := make([]byte, 0, n)
	buf := make([]byte, 32)
	limit := 256 / len(codeAlphabet) * len(codeAlphabet)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			// Отбрасываем «хвост» байта, чтобы все символы были равновероятны.
			if int(b) >= limit {
				continue
			}
			out = append(out, codeAlphabet[int(b)%len(codeAlphabet)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}

func normalizeCode(c string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(c)))
}

// beginCheck под a.mu проверяет блокировку и занимает адрес на время bcrypt.
// Возвращает текущий хэш пароля; после проверки вызвать endCheck.
func (a *Auth) beginCheck(client string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if at := a.failures[client]; at != nil && time.Now().Before(at.until) {
		return "", LockedError{at.until}
	}
	if a.state.Hash == "" {
		return "", ErrNoPassword
	}
	if a.checking[client] {
		return "", ErrBusy
	}
	a.checking[client] = true
	return a.state.Hash, nil
}

// Login проверяет пароль и возвращает токен новой сессии.
// bcrypt выполняется без a.mu: иначе каждая попытка (около секунды на MIPS)
// останавливала бы все запросы к API — Valid берёт ту же блокировку.
func (a *Auth) Login(client, password string) (string, error) {
	hash, err := a.beginCheck(client)
	if err != nil {
		return "", err
	}
	ok := compareHash([]byte(hash), []byte(password)) == nil
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.checking, client)
	if !ok {
		a.failLocked(client)
		return "", ErrWrong
	}
	if a.state.Hash != hash { // пароль успели сменить, пока шла проверка
		return "", ErrWrong
	}
	delete(a.failures, client)
	return a.newSessionLocked()
}

func (a *Auth) failLocked(client string) {
	now := time.Now()
	at := a.failures[client]
	if at == nil {
		if len(a.failures) >= maxTracked {
			a.pruneLocked(now)
		}
		at = &attempts{}
		a.failures[client] = at
	}
	at.count++
	at.last = now
	if at.count >= maxFailures {
		d := baseLockout << at.locks
		if d > maxLockout || d <= 0 {
			d = maxLockout
		}
		at.until, at.count = now.Add(d), 0
		at.locks++
	}
}

// pruneLocked забывает давние ошибки, а если таблица всё равно полна —
// вытесняет самую старую запись, в первую очередь незаблокированную.
func (a *Auth) pruneLocked(now time.Time) {
	for k, at := range a.failures {
		if now.After(at.until) && now.Sub(at.last) > forgetAfter {
			delete(a.failures, k)
		}
	}
	for len(a.failures) >= maxTracked {
		var victim string
		var vat *attempts
		for k, at := range a.failures {
			if vat == nil {
				victim, vat = k, at
				continue
			}
			locked, vlocked := now.Before(at.until), now.Before(vat.until)
			if (vlocked && !locked) || (vlocked == locked && at.last.Before(vat.last)) {
				victim, vat = k, at
			}
		}
		delete(a.failures, victim)
	}
}

// ChangePassword меняет пароль и завершает все остальные сессии.
func (a *Auth) ChangePassword(client, current, next, keepToken string) error {
	hash, err := a.beginCheck(client)
	if err != nil {
		return err
	}
	// Обе операции bcrypt — без блокировки.
	ok := compareHash([]byte(hash), []byte(current)) == nil
	var newHash string
	if ok {
		newHash, err = hashPassword(next)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.checking, client)
	if !ok {
		a.failLocked(client)
		return errors.New("текущий пароль указан неверно")
	}
	if err != nil {
		return err
	}
	if a.state.Hash != hash {
		return errors.New("пароль только что сменили, обновите страницу и повторите")
	}
	a.state.Hash = newHash
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

// hashPassword проверяет длину и считает bcrypt. Вызывать без a.mu.
func hashPassword(password string) (string, error) {
	if len([]rune(password)) < MinPassword {
		return "", ErrShort
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
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
	// Со сбросом на диск: пустой auth.json после сбоя питания означал бы
	// отказ запуска (или, без копии, ручное восстановление).
	return atomicfile.Write(a.path, data, 0o600)
}

func hashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}
