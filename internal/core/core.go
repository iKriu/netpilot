// Package core описывает подключаемые ядра прокси и выбор между ними.
package core

import (
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"netpilot/internal/model"
)

// Env — окружение, в котором запускается ядро.
type Env struct {
	DataDir   string
	OS        string
	ClashAddr string // куда ядру повесить Clash API ("" — не включать)
	CacheFile string // имя файла кэша (при перезагрузке старый экземпляр ещё держит прежний)
	ListsDir  string // скачанные списки (Re:filter и др.)
	// Selected — какой сервер сейчас выбран в автоматических группах:
	// тег группы → тег сервера. Выбирает NetPilot (см. app/autoswitch.go).
	Selected map[string]string
	// Compat113 — конфиг для sing-box 1.13 (на нём собран Hiddify Core):
	// без полей, появившихся в 1.14.
	Compat113 bool
}

// Reloader — ядро, которое умеет применить новую конфигурацию без остановки:
// порты не закрываются, и новые подключения не получают отказ.
// При ошибке прежняя конфигурация продолжает работать.
type Reloader interface {
	Reload(cfg *model.Config, env Env) error
}

// Exiter — ядро в отдельном процессе, которое может завершиться само (упасть).
// Приложение следит за этим: иначе оно считало бы ядро работающим, а в
// прозрачном режиме отправляло бы трафик сети в несуществующий процесс.
type Exiter interface {
	// Exited закрывается, когда процесс текущего запуска завершился
	// (в том числе после Stop). Брать сразу после успешного Start.
	Exited() <-chan struct{}
	// ExitReason — причина завершения и хвост журнала ядра.
	ExitReason() string
}

// Checker — ядро со своими ограничениями конфигурации сверх Capabilities
// (например, синтаксис правил). Validate добавляет его замечания.
type Checker interface {
	Check(cfg *model.Config) []Issue
}

// ErrReloadUnsupported — перезагрузка без остановки здесь невозможна, нужен перезапуск.
var ErrReloadUnsupported = errors.New("перезагрузка без остановки не поддерживается")

// Capabilities — что умеет ядро. Это данные, а не код: при обновлении
// ядра достаточно поправить список.
type Capabilities struct {
	Protocols  []string `json:"protocols"`
	Transports []string `json:"transports"`
	Security   []string `json:"security"`
	Embedded   bool     `json:"embedded"` // встроено в бинарник (без отдельного процесса)
	Lists      bool     `json:"lists"`    // понимает внешние списки (Re:filter)
	Detect     []string `json:"detect"`   // распознаёт протоколы по содержимому: bittorrent
	MemoryMB   int      `json:"memoryMb"` // примерный расход памяти
}

// Issue — то, что ядро не сможет выполнить из текущей конфигурации.
type Issue struct {
	NodeID  string `json:"nodeId,omitempty"`
	Message string `json:"message"`
}

type Core interface {
	Name() string
	Version() string
	Available() bool
	Capabilities() Capabilities
	// Render переводит единую модель в конфиг ядра (для запуска и предпросмотра).
	Render(cfg *model.Config, env Env) ([]byte, error)
	Start(cfg *model.Config, env Env) error
	Stop() error
	Running() bool
	// ClashAPI — адрес Clash-совместимого API ядра (задержки, соединения) или "".
	ClashAPI() string
}

// Validate проверяет, всё ли из конфигурации поддерживает ядро.
func Validate(c Core, cfg *model.Config) []Issue {
	caps := c.Capabilities()
	var issues []Issue
	for _, n := range cfg.Nodes {
		// Один битый сервер из подписки не должен ронять шлюз целиком:
		// такой сервер пропускается, остальные работают.
		if p := nodeProblem(n); p != "" {
			issues = append(issues, Issue{n.ID, fmt.Sprintf("«%s»: %s — сервер пропущен", n.Name, p)})
			continue
		}
		if !slices.Contains(caps.Protocols, n.Protocol) {
			issues = append(issues, Issue{n.ID, fmt.Sprintf("«%s»: протокол %s не поддерживается", n.Name, n.Protocol)})
			continue
		}
		if t := orDefault(n.Transport, "tcp"); !slices.Contains(caps.Transports, t) {
			issues = append(issues, Issue{n.ID, fmt.Sprintf("«%s»: транспорт %s не поддерживается", n.Name, t)})
		}
		if s := orDefault(n.Security, "none"); !slices.Contains(caps.Security, s) {
			issues = append(issues, Issue{n.ID, fmt.Sprintf("«%s»: %s не поддерживается", n.Name, s)})
		}
	}
	if !caps.Lists {
		for _, r := range cfg.Rules {
			if r.Enabled && len(r.What.Lists) > 0 {
				issues = append(issues, Issue{Message: fmt.Sprintf("правило «%s»: списки (Re:filter) поддерживают ядра sing-box и mihomo", r.Name)})
			}
		}
	}
	for _, r := range cfg.Rules {
		for _, p := range r.What.Protocols {
			if r.Enabled && !slices.Contains(caps.Detect, p) {
				issues = append(issues, Issue{Message: fmt.Sprintf("правило «%s»: распознавание %s есть в sing-box, Hiddify и Xray, в этом ядре правило не действует", r.Name, p)})
				break
			}
		}
	}
	if ch, ok := c.(Checker); ok {
		issues = append(issues, ch.Check(cfg)...)
	}
	return issues
}

// SingBoxBased — ядро с конфигом и Clash API sing-box: NetPilot сам выбирает
// сервер в его группах (встроенный sing-box и Hiddify).
func SingBoxBased(name string) bool { return name == "sing-box" || name == "hiddify" }

// Choice — результат выбора ядра с объяснением для интерфейса.
type Choice struct {
	Core   Core
	Reason string
	Issues []Issue
}

// Registry хранит все известные ядра.
type Registry struct {
	cores []Core
}

func (r *Registry) Register(c Core) { r.cores = append(r.cores, c) }

func (r *Registry) All() []Core { return r.cores }

func (r *Registry) Get(name string) Core {
	for _, c := range r.cores {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// Select выбирает ядро: явно заданное пользователем или лучшее под конфигурацию.
func (r *Registry) Select(cfg *model.Config) (Choice, error) {
	want := cfg.Settings.Core
	if want != "" && want != "auto" {
		c := r.Get(want)
		if c == nil {
			return Choice{}, fmt.Errorf("ядро %q не найдено", want)
		}
		if !c.Available() {
			return Choice{}, fmt.Errorf("ядро %q не установлено", want)
		}
		return Choice{Core: c, Reason: "выбрано вручную", Issues: Validate(c, cfg)}, nil
	}

	type cand struct {
		core   Core
		issues []Issue
	}
	var cands []cand
	for _, c := range r.cores {
		if c.Available() {
			cands = append(cands, cand{c, Validate(c, cfg)})
		}
	}
	if len(cands) == 0 {
		return Choice{}, fmt.Errorf("нет ни одного доступного ядра")
	}
	// Меньше проблем → встроенное → меньше памяти.
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if len(a.issues) != len(b.issues) {
			return len(a.issues) < len(b.issues)
		}
		ca, cb := a.core.Capabilities(), b.core.Capabilities()
		if ca.Embedded != cb.Embedded {
			return ca.Embedded
		}
		return ca.MemoryMB < cb.MemoryMB
	})
	best := cands[0]
	var reason string
	switch {
	case len(best.issues) > 0:
		reason = "ни одно ядро не поддерживает все серверы; выбрано с наименьшим числом проблем"
	case len(cands) == 1:
		reason = "единственное доступное ядро"
	default:
		reason = "поддерживает все серверы и расходует меньше ресурсов"
	}
	return Choice{Core: best.core, Reason: "авто: " + reason, Issues: best.issues}, nil
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// WithoutUnsupported убирает узлы, которые ядро не поддерживает (они уже показаны
// пользователю как проблемы). Ядро работает с остальными узлами, а не падает
// целиком и не создаёт заведомо неработающих подключений.
func WithoutUnsupported(cfg *model.Config, issues []Issue) *model.Config {
	if len(issues) == 0 {
		return cfg
	}
	bad := map[string]bool{}
	for _, is := range issues {
		if is.NodeID != "" {
			bad[is.NodeID] = true
		}
	}
	out := *cfg
	out.Nodes = nil
	for _, n := range cfg.Nodes {
		if !bad[n.ID] {
			out.Nodes = append(out.Nodes, n)
		}
	}
	return &out
}

// nodeProblem — явная ошибка в параметрах сервера, с которой ядро не запустится.
func nodeProblem(n model.Node) string {
	if strings.TrimSpace(n.Server) == "" {
		return "не указан адрес"
	}
	if n.Port <= 0 || n.Port > 65535 {
		return "неверный порт"
	}
	if n.Security == "reality" {
		k, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(n.PublicKey, "="))
		if err != nil || len(k) != 32 {
			return "неверный ключ Reality"
		}
	}
	return ""
}
