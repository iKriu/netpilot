// Package update — обновление самой программы из GitHub Releases.
//
// Программа только проверяет наличие новой версии; ставит её человек кнопкой.
// Установка безопасна: SHA256SUMS релиза проверяется по подписи ed25519
// (открытый ключ встроен в программу при сборке), файл сверяется с суммой
// из него, новая программа пробно запускается с -version, текущая сохраняется для
// отката. Если после установки новая версия несколько раз подряд не смогла
// проработать и минуты, при следующем запуске возвращается предыдущая.
package update

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"netpilot/internal/atomicfile"
)

// DefaultRepo — репозиторий с релизами по умолчанию (владелец/имя).
// Задаётся при сборке через -ldflags, в настройках можно указать другой.
var DefaultRepo = "iKriu/netpilot"

// PublicKey — открытый ключ ed25519 (base64), которым подписан SHA256SUMS
// каждого релиза. Задаётся при сборке:
//
//	-ldflags "-X netpilot/internal/update.PublicKey=..."
//
// Ключ зашит в программу, а не берётся из настроек: смена репозитория или
// захват релиза на GitHub не дают поставить чужую сборку. Пусто — сборка
// разработчика: подпись не проверяется, интерфейс об этом предупреждает.
var PublicKey = ""

const (
	checkEvery   = 6 * time.Hour
	firstCheck   = 2 * time.Minute
	healthyAfter = 90 * time.Second // столько новая версия должна проработать
	maxAttempts  = 3                // запусков без «здоровой» минуты до отката
	sumsAsset    = "SHA256SUMS"
	sigAsset     = sumsAsset + ".sig"
	// InstallTimeout — предел на всю установку: на медленном канале роутера
	// скачивание долгое, но зависнуть навсегда оно не должно.
	InstallTimeout = 15 * time.Minute
)

// Release — последняя опубликованная версия.
type Release struct {
	Version     string `json:"version"`
	Notes       string `json:"notes,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	Page        string `json:"page,omitempty"`
	Asset       string `json:"asset,omitempty"` // "" — нет сборки для этого устройства
	Size        int64  `json:"size,omitempty"`

	assetURL, sumsURL, sigURL string
}

type prefs struct {
	Repo      string   `json:"repo"`
	Token     string   `json:"token,omitempty"`
	AutoCheck *bool    `json:"autoCheck,omitempty"` // nil — включена
	CheckedAt int64    `json:"checkedAt,omitempty"`
	Latest    *Release `json:"latest,omitempty"`
	LastError string   `json:"lastError,omitempty"`
	Previous  string   `json:"previous,omitempty"` // версия, сохранённая для отката
}

type pending struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Attempts int    `json:"attempts"`
}

// State — то, что видит интерфейс.
type State struct {
	Current     string    `json:"current"`
	Platform    string    `json:"platform"`
	Asset       string    `json:"asset"`
	Repo        string    `json:"repo"`
	HasToken    bool      `json:"hasToken"`
	AutoCheck   bool      `json:"autoCheck"`
	CheckedAt   int64     `json:"checkedAt,omitempty"`
	Latest      *Release  `json:"latest,omitempty"`
	Available   bool      `json:"available"`
	Error       string    `json:"error,omitempty"`
	Previous    string    `json:"previous,omitempty"`
	Installing  bool      `json:"installing"`
	Progress    *Progress `json:"progress,omitempty"`
	CanRestart  bool      `json:"canRestart"`
	Development bool      `json:"development"`
	Signed      bool      `json:"signed"` // false — сборка без проверки подписи
}

// Progress — ход установки: этап и, при скачивании, сколько байт получено.
// Этапы: prepare, download, verify, probe, install, restart.
type Progress struct {
	Stage string `json:"stage"`
	Done  int64  `json:"done,omitempty"`
	Total int64  `json:"total,omitempty"`
}

type Updater struct {
	version string
	dir     string // data/update
	exe     string // путь к исполняемому файлу, определяется при старте

	mu          sync.Mutex
	p           prefs
	installing  bool
	rollingBack bool
	progress    Progress
	restart     chan struct{}
}

// New читает настройки и выполняет проверку после обновления: считает запуски
// новой версии и при повторных сбоях возвращает предыдущую.
func New(dataDir, version string) *Updater {
	u := &Updater{version: version, dir: filepath.Join(dataDir, "update"), restart: make(chan struct{}, 1)}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		u.exe = exe
	}
	_ = os.MkdirAll(u.dir, 0o755)
	if b, err := os.ReadFile(u.file("state.json")); err == nil {
		_ = json.Unmarshal(b, &u.p)
	}
	u.bootCheck()
	return u
}

func (u *Updater) file(name string) string { return filepath.Join(u.dir, name) }

func (u *Updater) save() {
	b, _ := json.MarshalIndent(u.p, "", "  ")
	_ = atomicfile.Write(u.file("state.json"), b, 0o600)
}

func (u *Updater) repo() string {
	if u.p.Repo != "" {
		return u.p.Repo
	}
	return DefaultRepo
}

func (u *Updater) autoCheck() bool { return u.p.AutoCheck == nil || *u.p.AutoCheck }

// Restart — сигнал главной программе: остановиться и запустить новую версию.
func (u *Updater) Restart() <-chan struct{} { return u.restart }

func (u *Updater) State() State {
	u.mu.Lock()
	defer u.mu.Unlock()
	st := State{
		Current: u.version, Platform: runtime.GOOS + "/" + arch(), Asset: assetName(),
		Repo: u.repo(), HasToken: u.p.Token != "", AutoCheck: u.autoCheck(),
		CheckedAt: u.p.CheckedAt, Latest: u.p.Latest, Error: u.p.LastError,
		Installing: u.installing, CanRestart: u.exe != "", Development: u.version == "dev",
		Signed: PublicKey != "",
	}
	if u.installing {
		pr := u.progress
		st.Progress = &pr
	}
	if u.p.Previous != "" && !sameVersion(u.p.Previous, u.version) && fileExists(u.file(prevName())) {
		st.Previous = u.p.Previous
	}
	if u.p.Latest != nil && u.p.Latest.Asset != "" {
		st.Available = newer(u.p.Latest.Version, u.version)
	}
	return st
}

// normRepo приводит репозиторий к виду владелец/имя; "" — репозиторий по умолчанию.
func normRepo(repo string) (string, error) {
	repo = strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(repo, "https://github.com/"), "github.com/")), "/")
	if repo != "" && strings.Count(repo, "/") != 1 {
		return "", errors.New("репозиторий указывается как владелец/имя, например iKriu/netpilot")
	}
	if repo == DefaultRepo {
		repo = ""
	}
	return repo, nil
}

// ChangesSource — меняют ли эти настройки источник обновлений (репозиторий
// или токен). Такое изменение подтверждается паролем.
func (u *Updater) ChangesSource(repo string, token *string) bool {
	repo, err := normRepo(repo)
	if err != nil {
		return false // SetPrefs всё равно откажет
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return repo != u.p.Repo || (token != nil && strings.TrimSpace(*token) != u.p.Token)
}

// SetPrefs меняет источник и автопроверку. token: nil — не менять, "" — удалить.
func (u *Updater) SetPrefs(repo string, token *string, autoCheck bool) error {
	repo, err := normRepo(repo)
	if err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if repo != u.p.Repo {
		u.p.Latest, u.p.LastError, u.p.CheckedAt = nil, "", 0
	}
	u.p.Repo = repo
	if token != nil {
		u.p.Token = strings.TrimSpace(*token)
	}
	u.p.AutoCheck = &autoCheck
	u.save()
	return nil
}

// Watch периодически проверяет наличие новой версии. Сама ничего не ставит.
func (u *Updater) Watch(ctx context.Context) {
	timer := time.NewTimer(firstCheck)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		u.mu.Lock()
		due := u.autoCheck() && time.Since(time.Unix(u.p.CheckedAt, 0)) >= checkEvery-time.Minute
		u.mu.Unlock()
		if due {
			if _, err := u.Check(ctx); err != nil {
				log.Printf("проверка обновлений: %v", err)
			} else if st := u.State(); st.Available {
				log.Printf("доступна новая версия NetPilot: %s (установлена %s)", st.Latest.Version, st.Current)
			}
		}
		timer.Reset(time.Hour)
	}
}

// Check запрашивает последний релиз.
func (u *Updater) Check(ctx context.Context) (State, error) {
	u.mu.Lock()
	repo, token := u.repo(), u.p.Token
	u.mu.Unlock()

	rel, err := fetchLatest(ctx, repo, token)
	u.mu.Lock()
	u.p.CheckedAt = time.Now().Unix()
	if err != nil {
		u.p.LastError = err.Error()
	} else {
		u.p.Latest, u.p.LastError = rel, ""
	}
	u.save()
	u.mu.Unlock()
	return u.State(), err
}

// Install скачивает, проверяет и ставит последнюю версию, затем просит
// программу перезапуститься.
func (u *Updater) Install(ctx context.Context) error {
	u.mu.Lock()
	if u.installing {
		u.mu.Unlock()
		return errors.New("обновление уже выполняется")
	}
	if u.rollingBack {
		u.mu.Unlock()
		return errors.New("выполняется возврат предыдущей версии")
	}
	if u.exe == "" {
		u.mu.Unlock()
		return errors.New("не удалось определить путь к программе")
	}
	u.installing = true
	u.progress = Progress{Stage: "prepare"}
	repo, token := u.repo(), u.p.Token
	u.mu.Unlock()
	restarting := false
	defer func() {
		// После успешной установки «идёт установка» держится до перезапуска.
		if !restarting {
			u.mu.Lock()
			u.installing = false
			u.mu.Unlock()
		}
	}()
	stage := func(name string) { u.mu.Lock(); u.progress = Progress{Stage: name}; u.mu.Unlock() }

	// Всегда свежие данные: релиз могли заменить после проверки.
	rel, err := fetchLatest(ctx, repo, token)
	if err != nil {
		return err
	}
	if rel.Asset == "" {
		return fmt.Errorf("в релизе %s нет сборки %s", rel.Version, assetName())
	}
	if !newer(rel.Version, u.version) {
		return fmt.Errorf("установлена актуальная версия %s", u.version)
	}

	sums, err := fetchSums(ctx, rel, token)
	if err != nil {
		return err
	}
	want := findSum(sums, rel.Asset)
	if want == "" {
		return fmt.Errorf("в %s нет суммы для %s", sumsAsset, rel.Asset)
	}

	tmp := filepath.Join(filepath.Dir(u.exe), ".netpilot-new"+filepath.Ext(u.exe))
	defer os.Remove(tmp)
	u.mu.Lock()
	u.progress = Progress{Stage: "download", Total: rel.Size}
	u.mu.Unlock()
	onBytes := func(n int64) { u.mu.Lock(); u.progress.Done = n; u.mu.Unlock() }
	if err := downloadFile(ctx, rel.assetURL, token, tmp, want, onBytes, func() { stage("verify") }); err != nil {
		return err
	}
	// Пробный запуск: сборка подходит к устройству и это действительно NetPilot нужной версии.
	stage("probe")
	if err := probe(ctx, tmp, rel.Version); err != nil {
		return err
	}
	stage("install")
	// Текущая версия — для отката.
	if err := copyFile(u.exe, u.file(prevName()), 0o755); err != nil {
		return fmt.Errorf("не удалось сохранить текущую версию: %w", err)
	}
	if err := replaceExe(u.exe, tmp); err != nil {
		return fmt.Errorf("не удалось заменить программу: %w", err)
	}

	u.mu.Lock()
	u.p.Previous = u.version
	u.save()
	u.mu.Unlock()
	writeJSON(u.file("pending.json"), pending{From: u.version, To: rel.Version})
	log.Printf("установлена версия %s (была %s), перезапуск", rel.Version, u.version)
	stage("restart")
	restarting = true
	u.requestRestart()
	return nil
}

// Rollback возвращает версию, сохранённую при последнем обновлении.
func (u *Updater) Rollback() error {
	u.mu.Lock()
	// Во время установки previous и сам исполняемый файл переписываются:
	// откат в этот момент оставил бы смесь двух версий.
	if u.installing {
		u.mu.Unlock()
		return errors.New("идёт установка обновления, вернуть предыдущую версию сейчас нельзя")
	}
	if u.rollingBack {
		u.mu.Unlock()
		return errors.New("возврат уже выполняется")
	}
	prev := u.p.Previous
	u.rollingBack = true
	u.mu.Unlock()
	defer func() { u.mu.Lock(); u.rollingBack = false; u.mu.Unlock() }()
	src := u.file(prevName())
	if prev == "" || !fileExists(src) {
		return errors.New("предыдущая версия не сохранена")
	}
	if u.exe == "" {
		return errors.New("не удалось определить путь к программе")
	}
	if err := u.restorePrevious(); err != nil {
		return err
	}
	os.Remove(u.file("pending.json"))
	u.mu.Lock()
	u.p.Previous = "" // возвращённая версия станет текущей
	u.save()
	u.mu.Unlock()
	log.Printf("возврат к версии %s", prev)
	u.requestRestart()
	return nil
}

func (u *Updater) restorePrevious() error {
	tmp := filepath.Join(filepath.Dir(u.exe), ".netpilot-prev"+filepath.Ext(u.exe))
	defer os.Remove(tmp)
	if err := copyFile(u.file(prevName()), tmp, 0o755); err != nil {
		return err
	}
	return replaceExe(u.exe, tmp)
}

func (u *Updater) requestRestart() {
	// Небольшая пауза, чтобы интерфейс успел получить ответ.
	time.AfterFunc(700*time.Millisecond, func() {
		select {
		case u.restart <- struct{}{}:
		default:
		}
	})
}

// bootCheck — защита от неудачного обновления.
func (u *Updater) bootCheck() {
	var p pending
	b, err := os.ReadFile(u.file("pending.json"))
	if err != nil || json.Unmarshal(b, &p) != nil {
		return
	}
	if !sameVersion(p.To, u.version) {
		os.Remove(u.file("pending.json"))
		return
	}
	p.Attempts++
	if p.Attempts <= maxAttempts {
		writeJSON(u.file("pending.json"), p)
		go func() {
			time.Sleep(healthyAfter)
			os.Remove(u.file("pending.json"))
			log.Printf("версия %s работает стабильно", u.version)
		}()
		return
	}
	log.Printf("версия %s не смогла стабильно запуститься %d раз, возвращаю %s", u.version, maxAttempts, p.From)
	os.Remove(u.file("pending.json"))
	if u.exe == "" || u.restorePrevious() != nil {
		log.Print("откат не удался: предыдущая версия недоступна")
		return
	}
	u.p.LastError = fmt.Sprintf("Версия %s не запустилась и была заменена на %s", p.To, p.From)
	u.save()
	reexec(u.exe)
}

// Reexec запускает установленную программу на месте текущей. Вызывается
// главной программой после остановки ядра и веб-сервера.
func (u *Updater) Reexec() { reexec(u.exe) }

// ---------- GitHub ----------

type ghRelease struct {
	TagName     string `json:"tag_name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		Size               int64  `json:"size"`
		URL                string `json:"url"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// Общего тайм-аута у клиента нет: сроки задают контексты (30 с на запрос
// к API, InstallTimeout на установку). Тайм-аут клиента обрывал бы
// скачивание сборки на медленном канале раньше, чем истечёт срок установки.
var client = &http.Client{}

// apiBase можно подменить для проверки на тестовом сервере.
var apiBase = func() string {
	if v := os.Getenv("NETPILOT_UPDATE_API"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.github.com"
}()

func fetchLatest(ctx context.Context, repo, token string) (*Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", apiBase+"/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "NetPilot")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub недоступен: %w", err)
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, errors.New("опубликованных версий пока нет")
	case http.StatusUnauthorized:
		return nil, errors.New("GitHub не принял токен")
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, errors.New("GitHub ограничил число запросов, повторите позже")
	default:
		return nil, fmt.Errorf("GitHub ответил %s", res.Status)
	}
	var gr ghRelease
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&gr); err != nil {
		return nil, fmt.Errorf("ответ GitHub: %w", err)
	}
	rel := &Release{
		Version: strings.TrimPrefix(gr.TagName, "v"), Notes: gr.Body,
		PublishedAt: gr.PublishedAt, Page: gr.HTMLURL,
	}
	link := func(url, browser string) string {
		if token != "" {
			return url // API-ссылка: работает и для приватных репозиториев
		}
		return browser
	}
	for _, a := range gr.Assets {
		switch a.Name {
		case assetName():
			rel.Asset, rel.Size, rel.assetURL = a.Name, a.Size, link(a.URL, a.BrowserDownloadURL)
		case sumsAsset:
			rel.sumsURL = link(a.URL, a.BrowserDownloadURL)
		case sigAsset:
			rel.sigURL = link(a.URL, a.BrowserDownloadURL)
		}
	}
	if rel.Asset != "" && rel.sumsURL == "" {
		return nil, fmt.Errorf("в релизе %s нет файла %s — ставить без проверки нельзя", rel.Version, sumsAsset)
	}
	// Сообщаем уже при проверке, а не только после нажатия «Обновить».
	if rel.Asset != "" && PublicKey != "" && rel.sigURL == "" {
		return nil, errUnsigned(rel.Version)
	}
	return rel, nil
}

func errUnsigned(version string) error {
	return fmt.Errorf("релиз %s не подписан (нет файла %s) — ставить его нельзя", version, sigAsset)
}

// fetchSums скачивает SHA256SUMS и, если в программу встроен ключ, проверяет
// его подпись. Суммы используются только после успешной проверки: подпись
// охватывает ровно скачанные байты, поэтому подменить ни одну строку нельзя.
func fetchSums(ctx context.Context, rel *Release, token string) ([]byte, error) {
	sums, err := download(ctx, rel.sumsURL, token, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("контрольные суммы: %w", err)
	}
	if PublicKey == "" {
		return sums, nil
	}
	if rel.sigURL == "" {
		return nil, errUnsigned(rel.Version)
	}
	sig, err := download(ctx, rel.sigURL, token, 4<<10)
	if err != nil {
		return nil, fmt.Errorf("подпись релиза: %w", err)
	}
	if err := verifySums(sums, sig); err != nil {
		return nil, err
	}
	return sums, nil
}

// verifySums проверяет подпись (base64 в файле .sig) встроенным ключом.
func verifySums(sums, sig []byte) error {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(PublicKey))
	if err != nil || len(key) != ed25519.PublicKeySize {
		// Сборка испорчена — безопаснее не ставить ничего.
		return errors.New("встроенный ключ проверки подписи повреждён — обновление невозможно, установите новую версию вручную")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return fmt.Errorf("файл %s повреждён — обновление отменено", sigAsset)
	}
	if !ed25519.Verify(ed25519.PublicKey(key), sums, raw) {
		return errors.New("подпись релиза не прошла проверку — файлы подменены или подписаны чужим ключом, обновление отменено")
	}
	return nil
}

func get(ctx context.Context, url, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "NetPilot")
	if token != "" && strings.HasPrefix(url, apiBase+"/") {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("загрузка: %s", res.Status)
	}
	return res, nil
}

func download(ctx context.Context, url, token string, limit int64) ([]byte, error) {
	res, err := get(ctx, url, token)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return io.ReadAll(io.LimitReader(res.Body, limit))
}

// downloadFile скачивает файл, сообщая о полученных байтах (onBytes), и сверяет сумму (перед этим — onVerify).
func downloadFile(ctx context.Context, url, token, dst, wantSum string, onBytes func(int64), onVerify func()) error {
	res, err := get(ctx, url, token)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h, &counter{fn: onBytes}), io.LimitReader(res.Body, 512<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("загрузка прервалась: %w", err)
	}
	onVerify()
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantSum) {
		return errors.New("контрольная сумма не совпала — файл повреждён или подменён, обновление отменено")
	}
	return nil
}

func findSum(sums []byte, name string) string {
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0]
		}
	}
	return ""
}

func probe(ctx context.Context, bin, want string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-version").Output()
	if err != nil {
		return fmt.Errorf("новая версия не запускается на этом устройстве: %w", err)
	}
	if got := strings.TrimSpace(string(out)); !sameVersion(got, want) {
		return fmt.Errorf("файл сообщает версию %q вместо %s — обновление отменено", got, want)
	}
	return nil
}

// ---------- платформа ----------

func arch() string {
	if runtime.GOARCH != "arm" {
		return runtime.GOARCH
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "GOARM" && s.Value != "" {
				return "armv" + strings.Split(s.Value, ",")[0]
			}
		}
	}
	return "armv7"
}

// assetName — имя файла сборки для этого устройства в релизе.
func assetName() string {
	n := "netpilot-" + runtime.GOOS + "-" + arch()
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	return n
}

func prevName() string {
	if runtime.GOOS == "windows" {
		return "previous.exe"
	}
	return "previous"
}

// replaceExe ставит новый файл на место работающей программы.
func replaceExe(exe, src string) error {
	if runtime.GOOS == "windows" {
		// Запущенный .exe нельзя перезаписать, но можно переименовать.
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		if err := os.Rename(src, exe); err != nil {
			_ = os.Rename(old, exe)
			return err
		}
		return nil
	}
	return os.Rename(src, exe)
}

// ---------- версии ----------

func parts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// newer — версия a новее установленной b. Сборка "dev" старше любой версии.
func newer(a, b string) bool {
	pa, pb := parts(a), parts(b)
	if pa == nil {
		return false
	}
	if pb == nil {
		return true
	}
	for i := 0; i < max(len(pa), len(pb)); i++ {
		x, y := at(pa, i), at(pb, i)
		if x != y {
			return x > y
		}
	}
	return false
}

func sameVersion(a, b string) bool {
	return strings.TrimPrefix(strings.TrimSpace(a), "v") == strings.TrimPrefix(strings.TrimSpace(b), "v")
}

func at(p []int, i int) int {
	if i < len(p) {
		return p[i]
	}
	return 0
}

// ---------- файлы ----------

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// writeJSON пишет атомарно и со сбросом на диск: pending.json читается после
// перезапуска (часто — после перезагрузки), и пустой файл вместо него сорвал бы
// проверку нового бинарника и откат.
func writeJSON(path string, v any) {
	b, _ := json.Marshal(v)
	_ = atomicfile.Write(path, b, 0o600)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if runtime.GOOS == "windows" {
		os.Remove(dst)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	// Данные уже сброшены (out.Sync), но само новое имя тоже должно
	// пережить сбой питания: иначе после перезагрузки копии для отката нет.
	atomicfile.SyncDir(filepath.Dir(dst))
	return nil
}

// counter считает записанные байты и сообщает о них не чаще раза в 64 КБ.
type counter struct {
	fn        func(int64)
	n, report int64
}

func (c *counter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	if c.n-c.report >= 64<<10 {
		c.report = c.n
		c.fn(c.n)
	}
	return len(p), nil
}
