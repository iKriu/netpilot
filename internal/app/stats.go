package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Статистика трафика по дням: всего, через прокси и напрямую, по устройствам.
// Копится в памяти и сохраняется в data/stats.json не чаще раза в 10 минут —
// на роутере частая запись изнашивает флеш. Хранятся последние statsDays дней.

const (
	statsDays      = 31
	statsSaveEvery = 10 * time.Minute
)

type DayStats struct {
	Date       string              `json:"date"` // 2026-10-01, местное время
	Up         int64               `json:"up"`
	Down       int64               `json:"down"`
	ProxyUp    int64               `json:"proxyUp"`
	ProxyDown  int64               `json:"proxyDown"`
	DirectUp   int64               `json:"directUp"`
	DirectDown int64               `json:"directDown"`
	Peak       float64             `json:"peak"` // наибольшая скорость загрузки, байт/с
	PeakAt     int64               `json:"peakAt,omitempty"`
	Devices    map[string][2]int64 `json:"devices,omitempty"` // IP → [отдано, получено]
}

type stats struct {
	mu     sync.Mutex
	loaded bool
	days   []*DayStats // по возрастанию даты
	dirty  bool
	saved  time.Time
}

func (a *App) statsPath() string { return filepath.Join(a.DataDir, "stats.json") }

func (s *stats) load(path string) {
	if s.loaded {
		return
	}
	s.loaded = true
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s.days)
	}
}

func (s *stats) today(now time.Time) *DayStats {
	date := now.Format("2006-01-02")
	if n := len(s.days); n > 0 && s.days[n-1].Date == date {
		return s.days[n-1]
	}
	d := &DayStats{Date: date, Devices: map[string][2]int64{}}
	s.days = append(s.days, d)
	if len(s.days) > statsDays {
		s.days = s.days[len(s.days)-statsDays:]
	}
	return d
}

// recordTraffic учитывает прирост трафика одного соединения.
func (a *App) recordTraffic(now time.Time, src string, proxied bool, up, down int64) {
	if up == 0 && down == 0 {
		return
	}
	s := &a.stats
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load(a.statsPath())
	d := s.today(now)
	d.Up += up
	d.Down += down
	if proxied {
		d.ProxyUp += up
		d.ProxyDown += down
	} else {
		d.DirectUp += up
		d.DirectDown += down
	}
	if d.Devices == nil {
		d.Devices = map[string][2]int64{}
	}
	v := d.Devices[src]
	d.Devices[src] = [2]int64{v[0] + up, v[1] + down}
	s.dirty = true
}

// recordPeak запоминает наибольшую скорость за день.
func (a *App) recordPeak(now time.Time, downRate float64) {
	s := &a.stats
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load(a.statsPath())
	d := s.today(now)
	if downRate > d.Peak {
		d.Peak, d.PeakAt = downRate, now.Unix()
		s.dirty = true
	}
}

// saveStats пишет статистику на диск, если пора.
func (a *App) saveStats(force bool) {
	s := &a.stats
	s.mu.Lock()
	if !s.dirty || (!force && time.Since(s.saved) < statsSaveEvery) {
		s.mu.Unlock()
		return
	}
	data, _ := json.Marshal(s.days)
	s.dirty, s.saved = false, time.Now()
	s.mu.Unlock()
	tmp := a.statsPath() + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, a.statsPath())
	}
}

type StatsView struct {
	Today DayStats   `json:"today"`
	Days  []DayStats `json:"days"` // без разбивки по устройствам
}

func (a *App) Stats() StatsView {
	s := &a.stats
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load(a.statsPath())
	v := StatsView{Today: *s.today(time.Now())}
	v.Today.Devices = make(map[string][2]int64, len(s.days[len(s.days)-1].Devices))
	for k, d := range s.days[len(s.days)-1].Devices {
		v.Today.Devices[k] = d
	}
	for _, d := range s.days {
		c := *d
		c.Devices = nil
		v.Days = append(v.Days, c)
	}
	sort.Slice(v.Days, func(i, j int) bool { return v.Days[i].Date < v.Days[j].Date })
	return v
}
