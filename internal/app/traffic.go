package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"netpilot/internal/core"
	"netpilot/internal/model"
)

// Трафик и соединения берутся из Clash-совместимого API ядра (sing-box, mihomo).
// Учитывается всё, что проходит через NetPilot; трафик устройств с маршрутом
// «напрямую» в прозрачном режиме идёт мимо ядра и здесь не виден.

type Connection struct {
	ID          string   `json:"id"`
	Network     string   `json:"network"`
	Inbound     string   `json:"inbound"`
	SourceIP    string   `json:"sourceIp"`
	Host        string   `json:"host"`
	Destination string   `json:"destination"` // IP:порт
	Up          int64    `json:"up"`
	Down        int64    `json:"down"`
	Start       int64    `json:"start"`
	Rule        string   `json:"rule"`
	Chains      []string `json:"chains"`
	// Для интерфейса.
	DeviceID   string `json:"deviceId,omitempty"`
	DeviceName string `json:"deviceName,omitempty"`
	Outbound   string `json:"outbound"`
}

type DeviceTraffic struct {
	Up       int64   `json:"up"`
	Down     int64   `json:"down"`
	UpRate   float64 `json:"upRate"`   // байт/с
	DownRate float64 `json:"downRate"` // байт/с
	Conns    int     `json:"conns"`
}

type traffic struct {
	mu       sync.Mutex
	last     map[string][2]int64 // id соединения → последние up/down
	meta     map[string]connMeta // id живого соединения → чьё оно и сколько дало за прошлый опрос
	total    [2]int64            // последние uploadTotal/downloadTotal ядра
	carry    [2]int64            // перебор: соединения насчитали больше, чем общий счётчик
	bySrc    map[string]*DeviceTraffic
	byOut    map[string]*DeviceTraffic // тег узла/выхода → трафик
	lastOut  map[string][2]int64       // Xray: последние накопленные значения по выходам
	conns    []Connection
	active   map[string]time.Time // IP источника → когда последний раз шёл трафик
	up       int64
	down     int64
	upRate   float64
	downRate float64
	updated  time.Time
	err      string
}

type clashConn struct {
	ID       string `json:"id"`
	Metadata struct {
		Network         string `json:"network"`
		Type            string `json:"type"`
		SourceIP        string `json:"sourceIP"`
		DestinationIP   string `json:"destinationIP"`
		DestinationPort string `json:"destinationPort"`
		Host            string `json:"host"`
	} `json:"metadata"`
	Upload   int64     `json:"upload"`
	Download int64     `json:"download"`
	Start    time.Time `json:"start"`
	Chains   []string  `json:"chains"`
	Rule     string    `json:"rule"`
	Payload  string    `json:"rulePayload"`
}

// connMeta — что известно о соединении, чтобы учесть его остаток после закрытия.
type connMeta struct {
	src     string
	proxied bool
	out     string
	up      int64 // прирост за прошлый опрос
	down    int64
}

// clashConnections — ответ /connections. Общие счётчики есть у sing-box и
// mihomo; указатели отличают «нет поля» от нуля.
type clashConnections struct {
	UploadTotal   *int64      `json:"uploadTotal"`
	DownloadTotal *int64      `json:"downloadTotal"`
	Connections   []clashConn `json:"connections"`
}

const trafficInterval = 2 * time.Second

// WatchTraffic опрашивает ядро и считает трафик по устройствам.
func (a *App) WatchTraffic(ctx context.Context) {
	t := time.NewTicker(trafficInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			a.saveStats(true)
			return
		case <-t.C:
			a.pollTraffic(ctx)
			a.saveStats(false)
		}
	}
}

func (a *App) clashAddr() core.Clash {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active == nil {
		return core.Clash{}
	}
	return a.active.ClashAPI()
}

// outboundStats — ядро без Clash API, но с накопленным трафиком по выходам (Xray).
type outboundStats interface {
	OutboundTraffic(ctx context.Context) (map[string][2]int64, error)
}

func (a *App) activeCore() any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.active
}

func (a *App) pollTraffic(ctx context.Context) {
	tr := &a.traffic
	api := a.clashAddr()
	if api.Addr == "" {
		if src, ok := a.activeCore().(outboundStats); ok {
			a.pollOutbounds(ctx, src)
			return
		}
		tr.mu.Lock()
		tr.conns, tr.upRate, tr.downRate = nil, 0, 0
		for _, d := range tr.bySrc {
			d.UpRate, d.DownRate, d.Conns = 0, 0, 0
		}
		for _, d := range tr.byOut {
			d.UpRate, d.DownRate, d.Conns = 0, 0, 0
		}
		tr.mu.Unlock()
		return
	}
	var body clashConnections
	if err := clashGet(ctx, api, "/connections", &body); err != nil {
		tr.mu.Lock()
		tr.err = err.Error()
		tr.mu.Unlock()
		return
	}
	a.applyConnections(time.Now(), body)
}

// applyConnections учитывает снимок /connections.
//
// Приросты живых соединений между опросами не видят байтов соединений,
// закрывшихся после прошлого опроса, и коротких (меньше trafficInterval).
// Поэтому общий трафик берётся из счётчиков ядра uploadTotal/downloadTotal,
// а разница с суммой по живым соединениям («остаток») распределяется по
// соединениям, закрывшимся с прошлого опроса, пропорционально их последнему
// приросту. Разбивка остатка по устройствам и выходам — оценка: последних
// байтов закрытого соединения ядро уже не сообщает. Общие цифры и сумма
// «через прокси + напрямую» в статистике при этом сходятся с ядром точно.
func (a *App) applyConnections(now time.Time, body clashConnections) {
	tr := &a.traffic
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.last == nil {
		tr.last, tr.bySrc = map[string][2]int64{}, map[string]*DeviceTraffic{}
	}
	if tr.byOut == nil {
		tr.byOut = map[string]*DeviceTraffic{}
	}
	if tr.active == nil {
		tr.active = map[string]time.Time{}
	}
	for _, o := range tr.byOut {
		o.UpRate, o.DownRate, o.Conns = 0, 0, 0
	}
	elapsed := now.Sub(tr.updated).Seconds()
	if tr.updated.IsZero() || elapsed <= 0 || elapsed > 10 {
		elapsed = trafficInterval.Seconds()
	}
	for _, d := range tr.bySrc {
		d.UpRate, d.DownRate, d.Conns = 0, 0, 0
	}
	// add учитывает прирост у устройства, выхода и в статистике дня.
	add := func(src string, proxied bool, out string, up, down int64, live bool) {
		if src != "" || live {
			d := tr.bySrc[src]
			if d == nil {
				d = &DeviceTraffic{}
				tr.bySrc[src] = d
			}
			d.Up += up
			d.Down += down
			d.UpRate += float64(up) / elapsed
			d.DownRate += float64(down) / elapsed
			if live {
				d.Conns++
			}
		}
		if out != "" {
			o := tr.byOut[out]
			if o == nil {
				o = &DeviceTraffic{}
				tr.byOut[out] = o
			}
			o.Up += up
			o.Down += down
			o.UpRate += float64(up) / elapsed
			o.DownRate += float64(down) / elapsed
			if live {
				o.Conns++
			}
		}
		a.recordTraffic(now, src, proxied, up, down)
	}

	next := make(map[string][2]int64, len(body.Connections))
	meta := make(map[string]connMeta, len(body.Connections))
	conns := make([]Connection, 0, len(body.Connections))
	var dUp, dDown int64
	for _, c := range body.Connections {
		prev := tr.last[c.ID]
		up, down := max(0, c.Upload-prev[0]), max(0, c.Download-prev[1])
		next[c.ID] = [2]int64{c.Upload, c.Download}
		src := c.Metadata.SourceIP
		tr.active[src] = now
		// Через прокси — всё, что ушло не на прямой выход.
		proxied := len(c.Chains) > 0 && c.Chains[0] != "direct" && c.Chains[0] != "DIRECT"
		out := ""
		if len(c.Chains) > 0 {
			out = c.Chains[0]
		}
		add(src, proxied, out, up, down, true)
		meta[c.ID] = connMeta{src: src, proxied: proxied, out: out, up: up, down: down}
		dUp += up
		dDown += down

		dest := c.Metadata.DestinationIP
		if dest != "" && c.Metadata.DestinationPort != "" {
			dest = joinHostPort(dest, c.Metadata.DestinationPort)
		}
		rule := c.Rule
		if c.Payload != "" {
			rule += " " + c.Payload
		}
		conns = append(conns, Connection{
			ID: c.ID, Network: c.Metadata.Network, Inbound: c.Metadata.Type, SourceIP: src,
			Host: hostPort(c.Metadata.Host, c.Metadata.DestinationPort), Destination: dest,
			Up: c.Upload, Down: c.Download, Start: c.Start.Unix(), Rule: rule, Chains: c.Chains,
		})
	}
	if body.UploadTotal != nil && body.DownloadTotal != nil {
		totUp := counterDelta(tr.total[0], *body.UploadTotal)
		totDown := counterDelta(tr.total[1], *body.DownloadTotal)
		tr.total = [2]int64{*body.UploadTotal, *body.DownloadTotal}
		// Счётчики соединений и общий читаются не одновременно: если соединения
		// насчитали больше, перебор вычитается из следующих остатков.
		var resUp, resDown int64
		resUp, tr.carry[0] = residual(totUp - dUp + tr.carry[0])
		resDown, tr.carry[1] = residual(totDown - dDown + tr.carry[1])
		if resUp > 0 || resDown > 0 {
			targets := closedConns(tr.meta, next)
			if len(targets) == 0 {
				// Соединения целиком прошли между опросами: скорее всего, от тех
				// же устройств, что сейчас активны.
				targets = closedConns(meta, nil)
			}
			if len(targets) == 0 {
				targets = []connMeta{{proxied: true}} // чьё — неизвестно
			}
			ups := split(resUp, targets, func(m connMeta) int64 { return m.up })
			downs := split(resDown, targets, func(m connMeta) int64 { return m.down })
			for i, m := range targets {
				if ups[i] > 0 || downs[i] > 0 {
					add(m.src, m.proxied, m.out, ups[i], downs[i], false)
				}
			}
			dUp += resUp
			dDown += resDown
		}
	}
	tr.last, tr.meta, tr.conns, tr.updated, tr.err = next, meta, conns, now, ""
	tr.up += dUp
	tr.down += dDown
	tr.upRate, tr.downRate = float64(dUp)/elapsed, float64(dDown)/elapsed
	a.recordPeak(now, tr.downRate)
}

// counterDelta — прирост накопленного счётчика. Ядро после перезапуска или
// перезагрузки конфигурации начинает счёт заново: новое значение меньше
// прежнего — значит, всё оно набежало с нуля.
func counterDelta(prev, cur int64) int64 {
	if cur < prev {
		return max(cur, 0)
	}
	return cur - prev
}

// residual делит остаток на учитываемую часть и перебор, переносимый дальше.
func residual(v int64) (count, carry int64) {
	if v < 0 {
		return 0, v
	}
	return v, 0
}

// closedConns — соединения из prev, которых нет в live (nil — все из prev),
// в постоянном порядке, чтобы делёж был воспроизводимым.
func closedConns(prev map[string]connMeta, live map[string][2]int64) []connMeta {
	ids := make([]string, 0, len(prev))
	for id := range prev {
		if _, ok := live[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]connMeta, len(ids))
	for i, id := range ids {
		out[i] = prev[id]
	}
	return out
}

// split делит total пропорционально весам (вес+1, чтобы без прироста тоже
// досталась доля); остаток от округления уходит последнему.
func split(total int64, items []connMeta, weight func(connMeta) int64) []int64 {
	out := make([]int64, len(items))
	if total <= 0 || len(items) == 0 {
		return out
	}
	var sum float64
	for _, m := range items {
		sum += float64(weight(m) + 1)
	}
	var given int64
	for i, m := range items[:len(items)-1] {
		out[i] = min(int64(float64(total)*float64(weight(m)+1)/sum), total-given)
		given += out[i]
	}
	out[len(items)-1] = total - given
	return out
}

// pollOutbounds — учёт по накопленным счётчикам выходов (Xray): общий трафик
// и трафик по узлам есть, а списка соединений и разбивки по устройствам нет.
func (a *App) pollOutbounds(ctx context.Context, src outboundStats) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	totals, err := src.OutboundTraffic(ctx)
	tr := &a.traffic
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if err != nil {
		tr.err = err.Error()
		return
	}
	now := time.Now()
	elapsed := now.Sub(tr.updated).Seconds()
	if tr.updated.IsZero() || elapsed <= 0 || elapsed > 10 {
		elapsed = trafficInterval.Seconds()
	}
	if tr.byOut == nil {
		tr.byOut = map[string]*DeviceTraffic{}
	}
	var dUp, dDown int64
	for tag, v := range totals {
		prev, seen := tr.lastOut[tag]
		up, down := v[0]-prev[0], v[1]-prev[1]
		if !seen || up < 0 || down < 0 { // первый опрос или ядро перезапущено
			up, down = 0, 0
		}
		o := tr.byOut[tag]
		if o == nil {
			o = &DeviceTraffic{}
			tr.byOut[tag] = o
		}
		o.Up += up
		o.Down += down
		o.UpRate, o.DownRate = float64(up)/elapsed, float64(down)/elapsed
		dUp += up
		dDown += down
	}
	tr.lastOut, tr.updated, tr.err, tr.conns = totals, now, "", nil
	tr.up += dUp
	tr.down += dDown
	tr.upRate, tr.downRate = float64(dUp)/elapsed, float64(dDown)/elapsed
	a.recordPeak(now, tr.downRate)
}

// OutboundView — трафик через узел или выход.
type OutboundView struct {
	Tag  string `json:"tag"`
	Name string `json:"name"`
	DeviceTraffic
}

type ConnectionsView struct {
	Available   bool           `json:"available"`
	Totals      bool           `json:"totals"` // общий трафик известен (даже без списка соединений)
	Outbounds   []OutboundView `json:"outbounds"`
	Reason      string         `json:"reason,omitempty"`
	Connections []Connection   `json:"connections"`
	Up          int64          `json:"up"`
	Down        int64          `json:"down"`
	UpRate      float64        `json:"upRate"`
	DownRate    float64        `json:"downRate"`
}

// Connections — текущие соединения с понятными названиями устройств и узлов.
func (a *App) Connections() ConnectionsView {
	cfg := a.Store.Get()
	v := ConnectionsView{Connections: []Connection{}}
	a.mu.Lock()
	switch {
	case a.active == nil:
		v.Reason = "Ядро остановлено"
	case a.active.ClashAPI().Addr == "":
		v.Reason = fmt.Sprintf("Ядро %s не сообщает список соединений — показан общий трафик и трафик по серверам", a.active.Name())
		_, v.Totals = a.active.(outboundStats)
	default:
		v.Available, v.Totals = true, true
	}
	a.mu.Unlock()

	tr := &a.traffic
	tr.mu.Lock()
	conns := append([]Connection(nil), tr.conns...)
	names0 := outboundNames(cfg)
	for tag, o := range tr.byOut {
		if o.Up+o.Down > 0 {
			v.Outbounds = append(v.Outbounds, OutboundView{Tag: tag, Name: outboundName(tag, names0), DeviceTraffic: *o})
		}
	}
	v.Up, v.Down, v.UpRate, v.DownRate = tr.up, tr.down, tr.upRate, tr.downRate
	if tr.err != "" && v.Available {
		v.Reason = tr.err
	}
	tr.mu.Unlock()

	byIP := devicesByIP(cfg)
	names := outboundNames(cfg)
	for i := range conns {
		c := &conns[i]
		if d, ok := byIP[c.SourceIP]; ok {
			c.DeviceID, c.DeviceName = d.ID, d.Name
		} else if isLoopback(c.SourceIP) {
			c.DeviceName = "Этот компьютер"
		}
		if len(c.Chains) > 0 {
			c.Outbound = outboundName(c.Chains[0], names)
		}
	}
	sort.Slice(conns, func(i, j int) bool { return conns[i].Start > conns[j].Start })
	sort.Slice(v.Outbounds, func(i, j int) bool {
		return v.Outbounds[i].Up+v.Outbounds[i].Down > v.Outbounds[j].Up+v.Outbounds[j].Down
	})
	v.Connections = conns
	return v
}

// activeSources — когда каждый IP последний раз пускал трафик через NetPilot.
func (a *App) activeSources() map[string]time.Time {
	tr := &a.traffic
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return maps.Clone(tr.active)
}

// TrafficBySource — трафик по IP-адресу источника.
func (a *App) TrafficBySource() map[string]DeviceTraffic {
	tr := &a.traffic
	tr.mu.Lock()
	defer tr.mu.Unlock()
	out := make(map[string]DeviceTraffic, len(tr.bySrc))
	for ip, d := range tr.bySrc {
		out[ip] = *d
	}
	return out
}

// CloseConnection закрывает соединение ("" — все).
func (a *App) CloseConnection(ctx context.Context, id string) error {
	api := a.clashAddr()
	if api.Addr == "" {
		return errors.New("ядро не поддерживает управление соединениями")
	}
	path := "/connections"
	if id != "" {
		path += "/" + url.PathEscape(id)
	}
	req, err := api.Request(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ядро ответило %s", resp.Status)
	}
	return nil
}

func clashGet(ctx context.Context, api core.Clash, path string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := api.Request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API ядра ответило %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func devicesByIP(cfg *model.Config) map[string]model.Device {
	m := make(map[string]model.Device, len(cfg.Devices))
	for _, d := range cfg.Devices {
		if d.IP != "" {
			m[d.IP] = d
		}
	}
	return m
}

// outboundNames переводит теги ядра в названия из интерфейса.
func outboundNames(cfg *model.Config) map[string]string {
	m := map[string]string{
		"direct": "Напрямую", "DIRECT": "Напрямую", "REJECT": "Заблокировано", "block": "Заблокировано",
		"proxy": "Прокси", "auto": "Авто",
	}
	for _, n := range cfg.Nodes {
		m["node-"+n.ID] = n.Name
	}
	for _, g := range cfg.Groups {
		m["group-"+g.ID] = g.Name
	}
	return m
}

func outboundName(tag string, names map[string]string) string {
	if n, ok := names[tag]; ok {
		return n
	}
	return tag
}

func hostPort(host, port string) string {
	if host == "" {
		return ""
	}
	if port == "" || port == "443" || port == "80" {
		return host
	}
	return host + ":" + port
}

func joinHostPort(h, p string) string {
	if strings.Contains(h, ":") {
		return "[" + h + "]:" + p
	}
	return h + ":" + p
}

func isLoopback(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1" || strings.HasPrefix(ip, "127.")
}
