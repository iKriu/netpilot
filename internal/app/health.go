package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"netpilot/internal/model"
)

// Проверка живучести шлюза. Процесс ядра может работать, а трафик — нет:
// повисло соединение с DNS-сервером, DoH перестал отвечать. В прозрачном
// режиме это значит, что у всей сети не открывается ни один сайт, а сторож
// процесса (watch_core.go) ничего не замечает. Поэтому NetPilot регулярно
// спрашивает у ядра имя — тем же путём, что и устройства, — и если ядро
// раз за разом не отвечает, а у самого сервера интернет есть:
//   - при «auto» переключает прямой DNS на следующий отвечающий сервер;
//   - иначе считает ядро сбойным: снимает перехват (с запасным DNS) и
//     перезапускает его, как после падения.

var (
	healthEvery    = 20 * time.Second
	healthFails    = 3                // столько неудачных проверок подряд — сбой
	dnsRetryBetter = 30 * time.Minute // как часто пробовать вернуться к приоритетному DNS
)

// directDNS — прямой DNS для ядра: при «auto» — выбранный из DirectDNSAuto.
func (a *App) directDNS() string {
	i := int(a.dnsIdx.Load())
	if i < 0 || i >= len(model.DirectDNSAuto) {
		i = 0
	}
	return model.DirectDNSAuto[i]
}

func dnsAuto(s model.Settings) bool { return s.DNSDirect == model.DNSAuto }

// pickDNS перед запуском ядра выбирает первый отвечающий DNS по приоритету.
// Вызывается без a.mu: проверка серверов занимает до нескольких секунд.
func (a *App) pickDNS(ctx context.Context) {
	if !dnsAuto(a.Store.Get().Settings) {
		return
	}
	if i := firstWorkingDNS(ctx, 0, len(model.DirectDNSAuto)); i >= 0 {
		a.setDNS(i, "выбран при запуске")
	}
}

func (a *App) setDNS(i int, why string) {
	if prev := int(a.dnsIdx.Swap(int32(i))); prev != i {
		log.Printf("прямой DNS: %s (%s)", model.DirectDNSAuto[i], why)
	}
}

// firstWorkingDNS — индекс первого отвечающего сервера из DirectDNSAuto[from:to], −1 — никто.
func firstWorkingDNS(ctx context.Context, from, to int) int {
	for i := from; i < to && i < len(model.DirectDNSAuto); i++ {
		if probeDoH(ctx, model.DirectDNSAuto[i]) == nil {
			return i
		}
	}
	return -1
}

// probeDoH — проверка DoH-сервера напрямую с этого компьютера (подменяется в тестах).
var probeDoH = func(ctx context.Context, server string) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	q, err := dnsQuery("www.google.com")
	if err != nil {
		return err
	}
	u := server + "?dns=" + base64.RawURLEncoding.EncodeToString(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/dns-message")
	// Без прокси из окружения: проверяем прямой путь, как у ядра.
	client := &http.Client{Transport: &http.Transport{Proxy: nil, ForceAttemptHTTP2: true}}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ответ %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return err
	}
	var m dnsmessage.Message
	if err := m.Unpack(body); err != nil {
		return fmt.Errorf("не DNS-ответ: %w", err)
	}
	return nil
}

func dnsQuery(name string) ([]byte, error) {
	n, err := dnsmessage.NewName(name + ".")
	if err != nil {
		return nil, err
	}
	m := dnsmessage.Message{
		Header:    dnsmessage.Header{RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: n, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}},
	}
	return m.Pack()
}

// uplinkUp — есть ли у сервера маршрут в интернет. UDP-«соединение» ничего не
// отправляет, но без маршрута сразу даёт «network is unreachable». Если кабель
// выдернут, перезапуск ядра и смена DNS бесполезны — сбой не засчитываем.
var uplinkUp = func() bool {
	c, err := net.DialTimeout("udp4", "1.1.1.1:53", time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// coreDNSWorks спрашивает у работающего ядра случайное имя через Clash API:
// ответ должен прийти от DNS-сервера, а не из кэша. checked=false — проверить
// нельзя (ядро остановлено, нет Clash API или в ядре нет такого метода).
func (a *App) coreDNSWorks(ctx context.Context) (ok, checked bool, gen uint64) {
	a.mu.Lock()
	act, gen := a.active, a.coreGen
	a.mu.Unlock()
	if act == nil {
		return false, false, gen
	}
	api := act.ClashAPI()
	if api.Addr == "" {
		return false, false, gen
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "np-" + hex.EncodeToString(b) + ".example.com"
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := api.Request(ctx, http.MethodGet, "/dns/query?name="+url.QueryEscape(name)+"&type=A", nil)
	if err != nil {
		return false, false, gen
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, true, gen
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return true, true, gen
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed:
		return false, false, gen // метода нет — судить не по чему
	}
	return false, true, gen
}

// WatchHealth следит, что ядро действительно пропускает трафик.
func (a *App) WatchHealth(ctx context.Context) {
	t := time.NewTicker(healthEvery)
	defer t.Stop()
	fails, lastBetter := 0, time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		ok, checked, gen := a.coreDNSWorks(ctx)
		if !checked || ok {
			fails = 0
			if checked && time.Since(lastBetter) >= dnsRetryBetter {
				lastBetter = time.Now()
				a.tryBetterDNS(ctx)
			}
			continue
		}
		if !uplinkUp() {
			fails = 0
			continue
		}
		if fails++; fails < healthFails {
			continue
		}
		fails = 0
		a.dnsUnhealthy(ctx, gen)
	}
}

// dnsUnhealthy — ядро несколько раз подряд не ответило на DNS-запрос.
func (a *App) dnsUnhealthy(ctx context.Context, gen uint64) {
	if dnsAuto(a.Store.Get().Settings) {
		cur := int(a.dnsIdx.Load())
		if probeDoH(ctx, a.directDNS()) != nil {
			// Сам сервер не отвечает — берём следующий отвечающий по приоритету.
			next := firstWorkingDNS(ctx, cur+1, len(model.DirectDNSAuto))
			if next < 0 {
				next = firstWorkingDNS(ctx, 0, cur)
			}
			if next >= 0 {
				a.setDNS(next, model.DirectDNSAuto[cur]+" не отвечает")
				if err := a.Apply(); err != nil {
					log.Printf("смена DNS: %v", err)
				}
				return
			}
		}
		// Сервер отвечает напрямую (или не отвечает никто) — зависло ядро.
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.coreGen != gen || a.active == nil {
		return // ядро уже перезапустили или остановили
	}
	name := a.active.Name()
	log.Printf("ядро %s не отвечает на DNS-запросы — перезапуск", name)
	a.coreFailedLocked(name, "перестало отвечать на DNS-запросы")
}

// tryBetterDNS возвращается к более приоритетному DNS, если тот снова отвечает.
func (a *App) tryBetterDNS(ctx context.Context) {
	if !dnsAuto(a.Store.Get().Settings) {
		return
	}
	cur := int(a.dnsIdx.Load())
	if cur == 0 {
		return
	}
	if i := firstWorkingDNS(ctx, 0, cur); i >= 0 {
		a.setDNS(i, "снова отвечает")
		if err := a.Apply(); err != nil {
			log.Printf("смена DNS: %v", err)
		}
	}
}
