package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"netpilot/internal/core"
	"netpilot/internal/model"
)

// Автоматические группы. Какой сервер выбран в группе, решает NetPilot, а не
// ядро: ядро проверяло бы все серверы группы пачками по 10 одновременно.
//
//   - Первый раз серверы группы проверяются по одному, выбирается быстрейший.
//     Пока проверка идёт, сразу берётся первый ответивший — чтобы сеть работала.
//   - Дальше раз в минуту проверяется только текущий сервер — один запрос.
//   - Переключение — только если текущий не ответил два раза подряд или
//     сбоил в трёх проверках из пяти. Тогда серверы перебираются по одному
//     в порядке прошлых задержек, и берётся первый ответивший.
//
// Выбор хранится в data/auto.json и передаётся ядру при каждом запуске
// (core.Env.Selected): применение настроек его не сбрасывает.

const (
	autoEvery   = time.Minute
	autoTimeout = 3000 // мс на проверку одного сервера
	autoHistory = 5
)

type autoState struct {
	mu       sync.Mutex
	loaded   bool
	selected map[string]string    // тег группы → id сервера
	history  map[string][]bool    // тег группы → последние проверки текущего сервера
	checked  map[string]time.Time // тег группы → последняя проверка
	lat      map[string]Latency   // id сервера → последний замер
	scan     map[string][2]int    // тег группы → идёт перебор: проверено, всего
	errs     map[string]string
	kick     chan struct{}
}

// AutoView — состояние автоматической группы для интерфейса.
type AutoView struct {
	GroupID   string `json:"groupId"` // "" — встроенная «Авто»
	Name      string `json:"name"`
	Members   int    `json:"members"`
	Selected  string `json:"selected,omitempty"` // id сервера
	Ms        int    `json:"ms"`                 // последняя задержка выбранного, -1 — не ответил
	CheckedAt int64  `json:"checkedAt,omitempty"`
	InUse     bool   `json:"inUse"`              // группа где-то используется — её и проверяем
	Manual    bool   `json:"manual,omitempty"`   // сервер выбирает человек, NetPilot не переключает
	Scanning  []int  `json:"scanning,omitempty"` // [проверено, всего]
	Error     string `json:"error,omitempty"`
}

func (s *autoState) init(dataDir string) {
	if s.loaded {
		return
	}
	s.loaded = true
	s.selected, s.history, s.checked = map[string]string{}, map[string][]bool{}, map[string]time.Time{}
	s.lat, s.scan, s.errs = map[string]Latency{}, map[string][2]int{}, map[string]string{}
	if b, err := os.ReadFile(filepath.Join(dataDir, "auto.json")); err == nil {
		_ = json.Unmarshal(b, &s.selected)
	}
}

// autoSelected — выбор для ядра: тег группы → тег сервера.
func (a *App) autoSelected() map[string]string {
	st := &a.auto
	st.mu.Lock()
	defer st.mu.Unlock()
	st.init(a.DataDir)
	out := make(map[string]string, len(st.selected))
	for g, id := range st.selected {
		out[g] = core.NodeTag(id)
	}
	return out
}

// kickAuto — проверить группы сейчас (после запуска ядра или применения настроек).
func (a *App) kickAuto() {
	st := &a.auto
	st.mu.Lock()
	if st.kick == nil {
		st.kick = make(chan struct{}, 1)
	}
	k := st.kick
	st.mu.Unlock()
	select {
	case k <- struct{}{}:
	default:
	}
}

// WatchAuto следит за автоматическими группами.
func (a *App) WatchAuto(ctx context.Context) {
	a.kickAuto() // создаёт канал
	t := time.NewTicker(autoEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-a.auto.kick:
			time.Sleep(3 * time.Second) // ядро только что запустилось
		}
		a.autoTick(ctx)
	}
}

type autoGroupInfo struct {
	Tag, ID, Name string
	Members       []string // id серверов
	InUse         bool
	Manual        bool // группа с ручным выбором: NetPilot её не проверяет и не переключает
}

// autoGroups — автоматические группы конфигурации и используются ли они:
// выбраны активным сервером, указаны в правиле или у устройства.
func autoGroups(cfg *model.Config) []autoGroupInfo {
	used := map[string]bool{}
	def := cfg.Settings.DefaultTarget
	if def == "" || (!slices.ContainsFunc(cfg.Nodes, func(n model.Node) bool { return n.ID == def }) &&
		!slices.ContainsFunc(cfg.Groups, func(g model.Group) bool { return g.ID == def })) {
		used[""] = true // активный сервер не выбран — работает встроенная «Авто»
	} else {
		used[def] = true
	}
	for _, r := range cfg.Rules {
		if r.Enabled && r.Where.Action == "proxy" && r.Where.Target != "" {
			used[r.Where.Target] = true
		}
	}
	for _, d := range cfg.Devices {
		if d.Route == "proxy" && d.RouteTarget != "" {
			used[d.RouteTarget] = true
		}
	}
	var out []autoGroupInfo
	if len(cfg.Nodes) > 1 {
		ids := make([]string, len(cfg.Nodes))
		for i, n := range cfg.Nodes {
			ids[i] = n.ID
		}
		out = append(out, autoGroupInfo{Tag: core.AutoTag, Name: "Авто", Members: ids, InUse: used[""]})
	}
	for _, g := range cfg.Groups {
		ids, err := core.GroupNodes(cfg, g)
		if err != nil || len(ids) == 0 {
			continue
		}
		out = append(out, autoGroupInfo{Tag: core.GroupTag(g.ID), ID: g.ID, Name: g.Name, Members: ids, InUse: used[g.ID], Manual: g.Type == "select"})
	}
	return out
}

func (a *App) autoTick(ctx context.Context) {
	a.mu.Lock()
	act := a.active
	a.mu.Unlock()
	// Выбором управляем только в sing-box: в mihomo группы — его собственный url-test.
	if act == nil || !core.SingBoxBased(act.Name()) || act.ClashAPI() == "" {
		return
	}
	cfg := a.Store.Get()
	for _, g := range autoGroups(cfg) {
		if !g.InUse || g.Manual || ctx.Err() != nil {
			continue
		}
		a.checkGroup(ctx, act.ClashAPI(), g, cfg.Settings.TestURL)
	}
}

func (a *App) checkGroup(ctx context.Context, api string, g autoGroupInfo, testURL string) {
	st := &a.auto
	st.mu.Lock()
	st.init(a.DataDir)
	cur := st.selected[g.Tag]
	st.mu.Unlock()

	if cur == "" || !slices.Contains(g.Members, cur) {
		a.scanGroup(ctx, api, g, testURL, "")
		return
	}
	l := a.measure(ctx, api, cur, testURL)
	st.mu.Lock()
	h := append(st.history[g.Tag], l.Ms > 0)
	if len(h) > autoHistory {
		h = h[len(h)-autoHistory:]
	}
	st.history[g.Tag], st.checked[g.Tag] = h, time.Now()
	st.mu.Unlock()

	failsInRow, fails := 0, 0
	for i := len(h) - 1; i >= 0 && !h[i]; i-- {
		failsInRow++
	}
	for _, ok := range h {
		if !ok {
			fails++
		}
	}
	if failsInRow >= 2 || fails >= 3 {
		a.failover(ctx, api, g, testURL, cur)
	}
}

// scanGroup — первый выбор: все серверы по одному, затем быстрейший.
func (a *App) scanGroup(ctx context.Context, api string, g autoGroupInfo, testURL, why string) {
	st := &a.auto
	best, bestMs, provisional := "", 0, false
	for i, id := range g.Members {
		if ctx.Err() != nil {
			return
		}
		st.mu.Lock()
		st.scan[g.Tag] = [2]int{i, len(g.Members)}
		st.mu.Unlock()
		l := a.measure(ctx, api, id, testURL)
		if l.Ms <= 0 {
			continue
		}
		if best == "" || l.Ms < bestMs {
			best, bestMs = id, l.Ms
		}
		// Пока перебор идёт, сеть должна работать: берём первый ответивший.
		if !provisional {
			provisional = true
			a.selectNode(ctx, api, g, id, "первый ответивший, проверка продолжается")
		}
	}
	st.mu.Lock()
	delete(st.scan, g.Tag)
	st.checked[g.Tag] = time.Now()
	if best == "" {
		st.errs[g.Tag] = "ни один сервер группы не ответил"
	}
	st.mu.Unlock()
	if best != "" {
		a.selectNode(ctx, api, g, best, fmt.Sprintf("быстрейший, %d мс", bestMs))
	}
}

// failover — текущий сервер сбоит: перебираем остальные по одному, начиная
// с самых быстрых по прошлым замерам, и берём первый ответивший.
func (a *App) failover(ctx context.Context, api string, g autoGroupInfo, testURL, cur string) {
	st := &a.auto
	st.mu.Lock()
	order := slices.DeleteFunc(slices.Clone(g.Members), func(id string) bool { return id == cur })
	lat := st.lat
	rank := func(id string) int {
		if l, ok := lat[id]; ok {
			if l.Ms > 0 {
				return l.Ms
			}
			return 1 << 30 // не отвечал — в конец
		}
		return 1 << 29 // не проверялся — перед неответившими
	}
	sort.SliceStable(order, func(i, j int) bool { return rank(order[i]) < rank(order[j]) })
	st.mu.Unlock()

	for i, id := range order {
		if ctx.Err() != nil {
			return
		}
		st.mu.Lock()
		st.scan[g.Tag] = [2]int{i, len(order)}
		st.mu.Unlock()
		if l := a.measure(ctx, api, id, testURL); l.Ms > 0 {
			st.mu.Lock()
			delete(st.scan, g.Tag)
			st.mu.Unlock()
			a.selectNode(ctx, api, g, id, fmt.Sprintf("прежний не отвечает, новый — %d мс", l.Ms))
			return
		}
	}
	st.mu.Lock()
	delete(st.scan, g.Tag)
	st.errs[g.Tag] = "текущий сервер не отвечает, другие тоже"
	st.mu.Unlock()
}

func (a *App) measure(ctx context.Context, api, id, testURL string) Latency {
	l := clashDelay(ctx, api, id, testURL, autoTimeout)
	st := &a.auto
	st.mu.Lock()
	st.lat[id] = l
	st.mu.Unlock()
	return l
}

func (a *App) selectNode(ctx context.Context, api string, g autoGroupInfo, id, why string) {
	st := &a.auto
	st.mu.Lock()
	prev := st.selected[g.Tag]
	st.selected[g.Tag] = id
	st.history[g.Tag] = nil
	delete(st.errs, g.Tag)
	data, _ := json.Marshal(st.selected)
	st.mu.Unlock()
	if prev != id {
		_ = os.WriteFile(filepath.Join(a.DataDir, "auto.json"), data, 0o600)
	}

	body, _ := json.Marshal(map[string]string{"name": core.NodeTag(id)})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, "http://"+api+"/proxies/"+url.PathEscape(g.Tag), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if res, err := http.DefaultClient.Do(req); err == nil {
		res.Body.Close()
	}
	if prev != id && a.Store != nil {
		name := id
		for _, n := range a.Store.Get().Nodes {
			if n.ID == id {
				name = n.Name
			}
		}
		log.Printf("группа «%s»: выбран сервер «%s» (%s)", g.Name, name, why)
	}
}

// AutoStatus — состояние автоматических групп и последние замеры серверов.
// SelectInGroup — человек выбрал сервер в группе. Выбор сохраняется и сразу
// применяется к запущенному ядру; у автоматической группы NetPilot дальше
// держится за этот сервер, пока тот отвечает.
func (a *App) SelectInGroup(ctx context.Context, groupID, nodeID string) error {
	cfg := a.Store.Get()
	for _, g := range autoGroups(cfg) {
		if g.ID != groupID || groupID == "" {
			continue
		}
		if !slices.Contains(g.Members, nodeID) {
			return fmt.Errorf("сервер не входит в группу «%s»", g.Name)
		}
		a.mu.Lock()
		act := a.active
		a.mu.Unlock()
		api := ""
		if act != nil && core.SingBoxBased(act.Name()) {
			api = act.ClashAPI()
		}
		if api == "" {
			a.storeSelection(g.Tag, nodeID)
			return nil
		}
		a.selectNode(ctx, api, g, nodeID, "выбран вручную")
		return nil
	}
	return fmt.Errorf("группа не найдена или в ней нет серверов")
}

// storeSelection запоминает выбор без запущенного ядра: он передастся при запуске.
func (a *App) storeSelection(tag, id string) {
	st := &a.auto
	st.mu.Lock()
	defer st.mu.Unlock()
	st.init(a.DataDir)
	st.selected[tag] = id
	st.history[tag] = nil
	data, _ := json.Marshal(st.selected)
	_ = os.WriteFile(filepath.Join(a.DataDir, "auto.json"), data, 0o600)
}

func (a *App) AutoStatus() ([]AutoView, map[string]Latency) {
	cfg := a.Store.Get()
	groups := autoGroups(cfg)
	st := &a.auto
	st.mu.Lock()
	defer st.mu.Unlock()
	st.init(a.DataDir)
	out := make([]AutoView, 0, len(groups))
	for _, g := range groups {
		v := AutoView{GroupID: g.ID, Name: g.Name, Members: len(g.Members), InUse: g.InUse, Manual: g.Manual, Error: st.errs[g.Tag]}
		id := st.selected[g.Tag]
		if g.Manual && !slices.Contains(g.Members, id) {
			id = g.Members[0] // пока не выбран — первый, как у ядра
		}
		if slices.Contains(g.Members, id) {
			v.Selected = id
			if l, ok := st.lat[id]; ok {
				v.Ms = l.Ms
			}
		}
		if t, ok := st.checked[g.Tag]; ok {
			v.CheckedAt = t.Unix()
		}
		if p, ok := st.scan[g.Tag]; ok {
			v.Scanning = []int{p[0], p[1]}
		}
		out = append(out, v)
	}
	lat := make(map[string]Latency, len(st.lat))
	for k, v := range st.lat {
		lat[k] = v
	}
	return out, lat
}
