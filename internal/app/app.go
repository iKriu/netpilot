// Package app связывает хранилище, ядра и API.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"netpilot/internal/core"
	"netpilot/internal/core/hiddify"
	"netpilot/internal/core/mihomo"
	"netpilot/internal/core/singbox"
	"netpilot/internal/core/xray"
	"netpilot/internal/dhcp"
	"netpilot/internal/links"
	"netpilot/internal/model"
	"netpilot/internal/router"
	"netpilot/internal/scan"
	"netpilot/internal/store"
)

// Version задаётся при сборке через -ldflags.
var Version = "dev"

type App struct {
	latest latestCache // последние версии ядер на GitHub

	Store   *store.Store
	Cores   *core.Registry
	DHCP    dhcp.Backend
	Router  router.Backend
	DataDir string

	net     network
	traffic traffic
	router  routerState
	auto    autoState
	stats   stats

	mu        sync.Mutex
	active    core.Core
	choice    core.Choice
	startedAt time.Time
	lastErr   string
	dirty     bool // конфигурация менялась после запуска ядра
	// dirtyBySubs — единственная причина dirty: автообновление подписок.
	// Тогда конфигурацию можно применить без участия пользователя.
	dirtyBySubs bool
	// coreGen — номер запуска ядра; меняется при каждом запуске и остановке,
	// чтобы отличать падение ядра от нашей остановки (см. watch_core.go).
	coreGen uint64
	restart restartState // перезапуск после падения ядра

	fwOn  bool   // перехват трафика включён
	fwErr string // последняя ошибка nftables
	dnsFB bool   // включён запасной DNS (ядро не работает, см. dnsFallbackLocked)

	// dnsIdx — какой сервер из model.DirectDNSAuto сейчас прямой DNS (при
	// «auto»). Атомарный: читается в env() под a.mu и меняется проверкой
	// живучести без него (health.go).
	dnsIdx atomic.Int32

	listErrs map[string]string // последние ошибки загрузки списков
}

func New(dataDir string) (*App, error) {
	st, err := store.Open(dataDir, func(c *model.Config) {
		// В контейнере прокси должен быть доступен снаружи.
		if v := os.Getenv("NETPILOT_PROXY_LISTEN"); v != "" {
			c.Settings.ListenAddr = v
		}
	})
	if err != nil {
		return nil, err
	}
	reg := &core.Registry{}
	reg.Register(singbox.New())
	reg.Register(xray.New(dataDir))
	reg.Register(mihomo.New(dataDir))
	reg.Register(hiddify.New(dataDir))
	cleanupFirewall()
	rb := router.Detect(dataDir)
	scan.ExtraLeaseFiles = []string{rb.LeaseFile()} // имена устройств из нашего DHCP-сервера
	return &App{Store: st, Cores: reg, DHCP: dhcp.Detect(), Router: rb, DataDir: dataDir}, nil
}

func (a *App) env() core.Env {
	return core.Env{DataDir: a.DataDir, OS: runtime.GOOS, ListsDir: a.listsDir(), Selected: a.autoSelected(), DeviceIPv6: a.allIPv6(), DNSDirect: a.directDNS()}
}

// allIPv6 — копия IPv6-адресов устройств: окружение ядра живёт дольше
// блокировки, а таблица соседей обновляется сканированием параллельно.
func (a *App) allIPv6() map[string][]string {
	a.net.mu.Lock()
	defer a.net.mu.Unlock()
	if len(a.net.v6) == 0 {
		return nil
	}
	out := make(map[string][]string, len(a.net.v6))
	for id, addrs := range a.net.v6 {
		out[id] = slices.Clone(addrs)
	}
	return out
}

// clashSecret — случайный ключ Clash API на один запуск ядра.
func clashSecret() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read не возвращает ошибок
	return hex.EncodeToString(b)
}

// Changed отмечает, что есть неприменённые изменения.
func (a *App) Changed() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active != nil {
		a.dirty, a.dirtyBySubs = true, false
	}
}

// subsChanged — то же, что Changed, но изменение пришло из подписки.
func (a *App) subsChanged() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active != nil {
		if !a.dirty {
			a.dirtyBySubs = true
		}
		a.dirty = true
	}
}

func (a *App) Start() error {
	a.ensureLists(context.Background(), listsMaxAge) // до блокировки: загрузка бывает долгой
	a.pickDNS(context.Background())                  // тоже до блокировки: проверка серверов
	a.mu.Lock()
	defer a.mu.Unlock()
	a.restart.fails = nil // запуск вручную — счёт сбоев с начала
	return a.startLocked()
}

func (a *App) startLocked() error {
	if a.active != nil {
		return errors.New("ядро уже запущено")
	}
	cfg := a.Store.Get()
	if transparent(cfg.Settings) && runtime.GOOS != "linux" {
		err := errors.New("прозрачный режим работает только на Linux (OpenWrt, ZimaOS и другие); на этой системе выберите режим «Прокси-сервер»")
		a.lastErr = err.Error()
		return err
	}
	choice, err := a.Cores.Select(cfg)
	if err != nil {
		a.lastErr = err.Error()
		return err
	}
	env := a.env()
	if addr, err := freeLocalAddr(); err == nil {
		env.ClashAddr, env.ClashSecret = addr, clashSecret()
	}
	if err := choice.Core.Start(core.WithoutUnsupported(cfg, choice.Issues), env); err != nil {
		a.lastErr = err.Error()
		return fmt.Errorf("%s: %w", choice.Core.Name(), err)
	}
	a.active, a.choice, a.startedAt, a.lastErr, a.dirty, a.dirtyBySubs = choice.Core, choice, time.Now(), "", false, false
	a.coreGen++
	a.restart.pending = false
	a.watchLocked(choice.Core)
	log.Printf("ядро %s %s запущено (%s)", choice.Core.Name(), choice.Core.Version(), choice.Reason)
	a.kickAuto()
	// Перехват включается только после того, как ядро готово принимать трафик.
	if err := a.applyFirewallLocked(); err != nil {
		_ = a.stopLocked()
		a.lastErr = "прозрачный режим: " + err.Error()
		return errors.New(a.lastErr)
	}
	if !transparent(cfg.Settings) {
		a.dnsFallbackLocked(false) // режим сменили на прокси-сервер
	}
	return nil
}

func (a *App) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stopLocked()
}

func (a *App) stopLocked() error {
	a.coreGen++ // завершение ядра теперь ожидаемое, назначенный перезапуск отменяется
	a.restart.pending = false
	if a.active == nil {
		// Ядро могло упасть при политике «блокировать интернет»: перехват остался.
		a.removeFirewallLocked()
		return nil
	}
	// Сначала снимаем перехват: иначе трафик устройств уйдёт в остановленное ядро.
	a.removeFirewallLocked()
	err := a.active.Stop()
	log.Printf("ядро %s остановлено", a.active.Name())
	a.active, a.dirty, a.dirtyBySubs = nil, false, false
	return err
}

// Apply применяет текущую конфигурацию. Если ядро то же и умеет перезагружаться,
// оно делает это без остановки (порты не закрываются, а при ошибке остаётся
// прежняя конфигурация). Иначе — перезапуск (ядро может смениться при «авто»).
func (a *App) Apply() error {
	a.ensureLists(context.Background(), listsMaxAge) // новые правила могли сослаться на новые списки
	a.mu.Lock()
	defer a.mu.Unlock()
	a.restart.fails = nil
	if a.active != nil {
		err := a.reloadLocked()
		if err == nil {
			return nil
		}
		if !errors.Is(err, core.ErrReloadUnsupported) {
			// Новая конфигурация не запустилась, прежняя продолжает работать.
			a.lastErr = "новая конфигурация не применена: " + err.Error()
			return errors.New(a.lastErr)
		}
	}
	if err := a.stopLocked(); err != nil {
		log.Printf("остановка ядра: %v", err)
	}
	return a.startLocked()
}

func (a *App) reloadLocked() error {
	cfg := a.Store.Get()
	if transparent(cfg.Settings) && runtime.GOOS != "linux" {
		return core.ErrReloadUnsupported // startLocked объяснит ошибку
	}
	choice, err := a.Cores.Select(cfg)
	if err != nil || choice.Core != a.active {
		return core.ErrReloadUnsupported
	}
	r, ok := a.active.(core.Reloader)
	if !ok {
		return core.ErrReloadUnsupported
	}
	env := a.env()
	if addr, err := freeLocalAddr(); err == nil {
		env.ClashAddr, env.ClashSecret = addr, clashSecret()
	}
	if err := r.Reload(core.WithoutUnsupported(cfg, choice.Issues), env); err != nil {
		return err
	}
	a.choice, a.lastErr, a.dirty, a.dirtyBySubs = choice, "", false, false
	// Режим или порт перехвата могли измениться.
	if transparent(cfg.Settings) {
		if err := a.applyFirewallLocked(); err != nil {
			return fmt.Errorf("прозрачный режим: %w", err)
		}
	} else {
		a.removeFirewallLocked()
		a.dnsFallbackLocked(false)
	}
	log.Printf("ядро %s: конфигурация применена без остановки", a.active.Name())
	a.kickAuto() // состав групп или активный сервер могли измениться
	return nil
}

type Status struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Running bool   `json:"running"`
	// Restarting — ядро упало и будет перезапущено (причина — в Error).
	Restarting bool         `json:"restarting,omitempty"`
	Core       string       `json:"core,omitempty"`
	CoreVer    string       `json:"coreVersion,omitempty"`
	Reason     string       `json:"reason,omitempty"`
	Issues     []core.Issue `json:"issues,omitempty"`
	StartedAt  int64        `json:"startedAt,omitempty"`
	Dirty      bool         `json:"dirty"`
	Error      string       `json:"error,omitempty"`
	Proxy      string       `json:"proxy"`
	LanIPs     []string     `json:"lanIps,omitempty"` // адреса NetPilot в локальной сети
	// Warnings — предупреждения не о ядре (например, настройки восстановлены
	// из резервной копии после сбоя питания): показываются на обзоре.
	Warnings []string `json:"warnings,omitempty"`
}

func (a *App) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.Store.Get().Settings
	st := Status{
		Version: Version, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Dirty: a.dirty, Error: a.lastErr, Restarting: a.restart.pending,
		Proxy:  net.JoinHostPort(s.ListenAddr, strconv.Itoa(s.MixedPort)),
		LanIPs: scan.LocalAddrs(),
	}
	if msg := a.Store.Recovered(); msg != "" {
		st.Warnings = append(st.Warnings, msg)
	}
	if a.active != nil {
		st.Running = true
		st.Core, st.CoreVer = a.active.Name(), a.active.Version()
		st.Reason, st.Issues = a.choice.Reason, a.choice.Issues
		st.StartedAt = a.startedAt.Unix()
	}
	return st
}

// Preview — конфиг, который получит ядро, и какое ядро будет выбрано.
func (a *App) Preview() (name, reason string, issues []core.Issue, data []byte, err error) {
	cfg := a.Store.Get()
	choice, err := a.Cores.Select(cfg)
	if err != nil {
		return "", "", nil, nil, err
	}
	data, err = choice.Core.Render(core.WithoutUnsupported(cfg, choice.Issues), a.env())
	return choice.Core.Name(), choice.Reason, choice.Issues, data, err
}

type CoreInfo struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Available    bool              `json:"available"`
	Installable  bool              `json:"installable"` // ставится отдельно (не встроено)
	Stats        bool              `json:"stats"`       // сообщает список соединений (Clash API)
	Capabilities core.Capabilities `json:"capabilities"`
	Issues       []core.Issue      `json:"issues"`
	// Latest — последняя стабильная версия на GitHub ("" — ещё не проверено).
	Latest string `json:"latest,omitempty"`
}

func (a *App) CoreList() []CoreInfo {
	cfg := a.Store.Get()
	latest := a.coreLatest()
	var out []CoreInfo
	for _, c := range a.Cores.All() {
		out = append(out, CoreInfo{
			Name: c.Name(), Version: c.Version(), Available: c.Available(),
			Installable: isInstallable(c), Stats: c.Name() != "xray",
			Capabilities: c.Capabilities(), Issues: core.Validate(c, cfg),
			Latest: latest[c.Name()],
		})
	}
	return out
}

// Latency — задержка сервера. Если ядро запущено — реальным HTTP-запросом
// через сервер (Clash API), иначе — временем TCP-подключения к нему.
type Latency struct {
	ID     string `json:"id"`
	Ms     int    `json:"ms"` // -1 — недоступен
	Method string `json:"method"`
	Error  string `json:"error,omitempty"`
}

func (a *App) TestNodes(ctx context.Context, ids []string) []Latency {
	cfg := a.Store.Get()
	a.mu.Lock()
	var clash core.Clash
	if a.active != nil && !a.dirty {
		clash = a.active.ClashAPI()
	}
	a.mu.Unlock()
	method := cfg.Settings.PingMethod
	if method == "" || method == PingAuto {
		method = PingTCP
		if clash.Addr != "" {
			method = PingHTTP
		}
	}

	var nodes []model.Node
	for _, n := range cfg.Nodes {
		if len(ids) == 0 || contains(ids, n.ID) {
			nodes = append(nodes, n)
		}
	}
	// Строго по одному: пачка одновременных проверок — всплеск трафика
	// через все серверы сразу. Интерфейс сам просит серверы по одному
	// и показывает результат каждого сразу.
	out := make([]Latency, 0, len(nodes))
	for _, n := range nodes {
		if ctx.Err() != nil {
			break
		}
		var l Latency
		switch method {
		case PingHTTP:
			if clash.Addr == "" {
				l = Latency{ID: n.ID, Ms: -1, Method: PingHTTP, Error: "запустите шлюз: проверка идёт через ядро"}
			} else {
				l = clashDelay(ctx, clash, n.ID, cfg.Settings.TestURL, 5000)
			}
		case PingICMP:
			l = icmpPing(ctx, n)
		default:
			l = tcpPing(ctx, n)
		}
		a.auto.mu.Lock()
		if a.auto.lat != nil {
			a.auto.lat[n.ID] = l
		}
		a.auto.mu.Unlock()
		out = append(out, l)
	}
	return out
}

func clashDelay(ctx context.Context, api core.Clash, id, testURL string, timeoutMs int) Latency {
	res := Latency{ID: id, Ms: -1, Method: "http"}
	if testURL == "" {
		testURL = "https://www.gstatic.com/generate_204"
	}
	path := fmt.Sprintf("/proxies/%s/delay?timeout=%d&url=%s",
		url.PathEscape(singboxNodeTag(id)), timeoutMs, url.QueryEscape(testURL))
	req, err := api.Request(ctx, http.MethodGet, path, nil)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()
	var body struct {
		Delay   int    `json:"delay"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusOK || body.Delay <= 0 {
		res.Error = body.Message
		if res.Error == "" {
			res.Error = resp.Status
		}
		return res
	}
	res.Ms = body.Delay
	return res
}

// Теги узлов совпадают у всех ядер, которые поддерживают Clash API.
func singboxNodeTag(id string) string { return "node-" + id }

func tcpPing(ctx context.Context, n model.Node) Latency {
	res := Latency{ID: n.ID, Ms: -1, Method: PingTCP}
	if n.Protocol == "hysteria2" || n.Protocol == "tuic" {
		res.Error = "UDP-протокол: проверьте через ICMP или HTTP через сервер"
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(n.Server, strconv.Itoa(n.Port)))
	if err != nil {
		res.Error = err.Error()
		return res
	}
	_ = conn.Close()
	res.Ms = max(1, int(time.Since(start).Milliseconds()))
	return res
}

// ImportLinks добавляет узлы из текста со ссылками.
func (a *App) ImportLinks(text, subID string) (added int, errs []string, err error) {
	nodes, perrs := links.ParseMany(text)
	for _, e := range perrs {
		errs = append(errs, e.Error())
	}
	err = a.Store.Update(func(c *model.Config) error {
		for i := range nodes {
			nodes[i].ID = store.NewID()
			nodes[i].SubscriptionID = subID
			if nodes[i].Name == "" {
				nodes[i].Name = fmt.Sprintf("%s %s", nodes[i].Protocol, nodes[i].Server)
			}
			c.Nodes = append(c.Nodes, nodes[i])
		}
		return nil
	})
	if err == nil && len(nodes) > 0 {
		a.Changed()
	}
	return len(nodes), errs, err
}

// CoreLog возвращает хвост журнала ядра.
func (a *App) CoreLog(maxBytes int64) string {
	f, err := os.Open(filepath.Join(a.DataDir, "core.log"))
	if err != nil {
		return ""
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > maxBytes {
		_, _ = f.Seek(st.Size()-maxBytes, io.SeekStart)
	}
	b, _ := io.ReadAll(f)
	return string(b)
}

func freeLocalAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return l.Addr().String(), nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
