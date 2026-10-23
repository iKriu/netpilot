package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"netpilot/internal/core"
	"netpilot/internal/firewall"
	"netpilot/internal/model"
)

// Проверка живучести шлюза. Процесс ядра может работать, а трафик — нет:
// повисло соединение с DNS-сервером, DoH перестал отвечать. В прозрачном
// режиме это значит, что у всей сети не открывается ни один сайт, а сторож
// процесса (watch_core.go) ничего не замечает. Поэтому NetPilot регулярно
// спрашивает у ядра имя — тем же путём, что и устройства, — и если ядро
// раз за разом не отвечает, а у самого сервера интернет есть:
//   - при «auto» переключает прямой DNS на следующий отвечающий сервер;
//   - иначе считает ядро зависшим и перезапускает его (не чаще, чем
//     позволяют healthRestartGaps).
//
// Если интернета нет у самого сервера (авария у провайдера, шлюз по
// умолчанию — сам сервер), ничего не делается: перезапуск ядра это не лечит.

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

// pickDNS перед запуском ядра выбирает отвечающий DNS с наивысшим приоритетом.
// Вызывается без a.mu: проверка серверов занимает до probeTimeout.
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

// probeTimeout — сколько ждать ответа DoH-серверов при выборе. Серверы
// проверяются одновременно, поэтому запуск ядра ждёт не дольше этого, даже
// если половина списка не отвечает (раньше — до 4 с на каждый по очереди).
var probeTimeout = 3 * time.Second

// firstWorkingDNS — индекс отвечающего сервера с наивысшим приоритетом из
// DirectDNSAuto[from:to], −1 — никто. Все кандидаты проверяются параллельно.
func firstWorkingDNS(ctx context.Context, from, to int) int {
	to = min(to, len(model.DirectDNSAuto))
	if from >= to {
		return -1
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	ok := make([]bool, to-from)
	var wg sync.WaitGroup
	for i := from; i < to; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok[i-from] = probeDoH(ctx, model.DirectDNSAuto[i]) == nil
		}(i)
	}
	wg.Wait()
	for i, v := range ok {
		if v {
			return from + i
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

// uplinkUp — есть ли у сервера выход в интернет; если нет — причина. Если
// кабель выдернут, у провайдера авария или маршрут по умолчанию ведёт в сам
// сервер, перезапуск ядра и смена DNS бесполезны — сбой не засчитываем, иначе
// ядро перезапускалось бы по кругу, а настоящая причина терялась бы в журнале.
//   - Маршруты читаются из таблицы ядра Linux: шлюз по умолчанию, равный адресу
//     этого компьютера (роутер раздал NetPilot шлюзом и самому серверу), или
//     отсутствие маршрута по умолчанию — выхода в интернет нет.
//   - Затем — настоящие TCP-соединения с несколькими узлами. UDP-«соединение»
//     не годится: оно ничего не отправляет и «удаётся» при любом маршруте,
//     даже через самого себя или к упавшему провайдеру. Узлы разные (в том
//     числе российский), чтобы блокировка одного не выглядела аварией.
//     Собственный трафик сервера прозрачный режим не перехватывает, так что
//     это проверка прямого пути, а не ядра.
var uplinkUp = func() (bool, string) {
	if why := firewall.Uplink(); why != "" {
		return false, why
	}
	if reachable(uplinkTargets, 3*time.Second) {
		return true, ""
	}
	return false, "сервер не может соединиться ни с одним узлом в интернете"
}

var uplinkTargets = []string{"1.1.1.1:443", "8.8.8.8:443", "77.88.8.8:443"}

// reachable — удалось ли TCP-соединение хотя бы с одним адресом (параллельно).
func reachable(addrs []string, timeout time.Duration) bool {
	res := make(chan bool, len(addrs))
	d := net.Dialer{Timeout: timeout}
	for _, addr := range addrs {
		go func(addr string) {
			c, err := d.Dial("tcp", addr)
			if err == nil {
				c.Close()
			}
			res <- err == nil
		}(addr)
	}
	for range addrs {
		if <-res {
			return true
		}
	}
	return false
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
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
	ok, checked = dnsQueryAnswered(resp.StatusCode, body)
	return ok, checked, gen
}

// dnsQueryAnswered оценивает ответ Clash API на /dns/query. Имя случайное и
// заведомо не существует, поэтому «ошибка» от DNS-сервера (NXDOMAIN — ядро
// может отдать её как 5xx с текстом вроде «rcode: name error») — нормальный
// ответ: DNS работает. Сбоем считается всё остальное — таймаут, обрыв
// соединения, нет маршрута, непонятная ошибка: ложный «работает» опаснее
// лишней проверки (до перезапуска нужно healthFails неудач подряд).
func dnsQueryAnswered(status int, body []byte) (ok, checked bool) {
	switch {
	case status == http.StatusOK:
		return true, true
	case status == http.StatusNotFound || status == http.StatusMethodNotAllowed:
		return false, false // метода нет — судить не по чему
	case status >= 500:
		msg := strings.ToLower(string(body))
		for _, answered := range []string{"rcode", "nxdomain", "name error", "no such host"} {
			if strings.Contains(msg, answered) {
				return true, true // DNS-сервер ответил «нет такого имени» — значит, ответил
			}
		}
	}
	return false, true
}

// WatchHealth следит, что ядро действительно пропускает трафик.
func (a *App) WatchHealth(ctx context.Context) {
	t := time.NewTicker(healthEvery)
	defer t.Stop()
	fails, lastBetter := 0, time.Now()
	uplinkWhy := "" // причина последнего «нет интернета у сервера» — пишем в журнал один раз
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
				a.tryBetterDNS(ctx, gen)
			}
			continue
		}
		up, why := uplinkUp()
		if !up {
			fails = 0
			if why != uplinkWhy {
				log.Printf("ядро не отвечает на DNS-запросы, но у самого сервера нет интернета (%s) — перезапуск ядра не поможет, жду восстановления сети", why)
				uplinkWhy = why
			}
			continue
		}
		if uplinkWhy != "" {
			log.Print("выход сервера в интернет восстановлен")
			uplinkWhy = ""
		}
		if fails++; fails < healthFails {
			continue
		}
		fails = 0
		a.dnsUnhealthy(ctx, gen)
	}
}

// dnsUnhealthy — ядро несколько раз подряд не ответило на DNS-запрос.
// gen — номер запуска ядра на момент проверки: всё, что делается после
// медленных проб, относится только к этому запуску (см. reloadIfGen).
func (a *App) dnsUnhealthy(ctx context.Context, gen uint64) {
	if dnsAuto(a.Store.Get().Settings) {
		cur := int(a.dnsIdx.Load())
		if probeDoH(ctx, a.directDNS()) != nil {
			// Сам сервер не отвечает — берём следующий отвечающий по приоритету.
			next := firstWorkingDNS(ctx, cur+1, len(model.DirectDNSAuto))
			if next < 0 {
				next = firstWorkingDNS(ctx, 0, cur)
			}
			if next < 0 {
				// Напрямую не отвечает ни один DNS — значит, нет интернета у
				// самого сервера (авария у провайдера), а не зависло ядро.
				log.Print("ядро не отвечает на DNS-запросы, но и напрямую не отвечает ни один DNS-сервер — похоже, нет интернета; ядро не перезапускаю")
				return
			}
			a.setDNS(next, model.DirectDNSAuto[cur]+" не отвечает")
			a.reloadIfGen(gen)
			return
		}
		// Сервер отвечает напрямую — зависло ядро.
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.coreGen != gen || a.active == nil {
		return // ядро уже перезапустили или остановили
	}
	name := a.active.Name()
	if wait := a.healthRestartWaitLocked(time.Now()); wait > 0 {
		log.Printf("ядро %s не отвечает на DNS-запросы; недавно уже перезапускалось — следующая попытка не раньше чем через %d мин", name, int(wait.Minutes())+1)
		return
	}
	a.tp.healthRestarts = append(a.tp.healthRestarts, time.Now())
	log.Printf("ядро %s не отвечает на DNS-запросы — перезапуск", name)
	a.restartLocked(name, "перестало отвечать на DNS-запросы")
}

// Паузы между перезапусками по проверке живучести: первый — сразу, затем не
// чаще, чем через 2, 10, 30 минут. Счёт отдельный от падений ядра
// (restartMaxFails): зависание, которое перезапуск не лечит, не должно
// исчерпать попытки и навсегда оставить ядро без перезапуска после падения.
var healthRestartGaps = []time.Duration{2 * time.Minute, 10 * time.Minute, 30 * time.Minute}

const healthRestartWindow = 2 * time.Hour

// healthRestartWaitLocked — сколько ещё ждать до следующего перезапуска по
// проверке живучести (0 — можно сейчас).
func (a *App) healthRestartWaitLocked(now time.Time) time.Duration {
	recent := a.tp.healthRestarts[:0]
	for _, t := range a.tp.healthRestarts {
		if now.Sub(t) < healthRestartWindow {
			recent = append(recent, t)
		}
	}
	a.tp.healthRestarts = recent
	if len(recent) == 0 {
		return 0
	}
	gap := healthRestartGaps[min(len(recent), len(healthRestartGaps))-1]
	return max(0, gap-now.Sub(recent[len(recent)-1]))
}

// restartLocked — остановка и запуск ядра без запасного DNS в промежутке.
// Если запуск не удался — обычный перезапуск после сбоя (с паузами).
func (a *App) restartLocked(name, what string) {
	a.tp.hold = true
	if err := a.stopLocked(); err != nil {
		log.Printf("остановка ядра: %v", err)
	}
	a.tp.hold = false
	if err := a.startLocked(); err != nil {
		log.Printf("перезапуск ядра %s: %v", name, err)
		a.scheduleRestartLocked(name, what+"; не перезапустилось: "+err.Error())
	}
}

// reloadTimeout — сколько ждать перезагрузки конфигурации ядра, начатой
// проверкой живучести. Ядро в этот момент, возможно, зависло: ждать без
// предела нельзя.
var reloadTimeout = 30 * time.Second

var errReloadTimeout = errors.New("ядро не применило настройки вовремя")

// reloadIfGen применяет конфигурацию (новый прямой DNS) к запуску ядра gen —
// только если он ещё работает. В отличие от Apply() не запускает ядро, которое
// пользователь остановил, не обходит назначенный перезапуск и не сбрасывает
// счёт сбоев. Сама перезагрузка идёт без a.mu и с пределом по времени:
// зависшее ядро не должно держать блокировку (а с ней и панель).
func (a *App) reloadIfGen(gen uint64) {
	a.mu.Lock()
	if a.coreGen != gen || a.active == nil {
		a.mu.Unlock()
		return
	}
	cfg := a.Store.Get()
	choice, err := a.Cores.Select(cfg)
	r, ok := a.active.(core.Reloader)
	if err != nil || choice.Core != a.active || !ok || (transparent(cfg.Settings) && runtime.GOOS != "linux") {
		// Перезагрузка на лету невозможна — перезапуск.
		a.restartLocked(a.active.Name(), "не применило новый DNS")
		a.mu.Unlock()
		return
	}
	env := a.env()
	if addr, err := freeLocalAddr(); err == nil {
		env.ClashAddr, env.ClashSecret = addr, clashSecret()
	}
	a.mu.Unlock()

	done := make(chan error, 1)
	go func() { done <- r.Reload(core.WithoutUnsupported(cfg, choice.Issues), env) }()
	select {
	case err = <-done:
	case <-time.After(reloadTimeout):
		err = errReloadTimeout
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.coreGen != gen || a.active != choice.Core {
		return // пока шла перезагрузка, ядро остановили или перезапустили
	}
	name := a.active.Name()
	switch {
	case errors.Is(err, errReloadTimeout):
		log.Printf("ядро %s не применило настройки за %d с — считаю зависшим", name, int(reloadTimeout.Seconds()))
		a.coreFailedLocked(name, "зависло при смене DNS")
		return
	case err != nil:
		log.Printf("смена DNS: %v", err) // прежняя конфигурация продолжает работать
		return
	}
	a.choice = choice
	if transparent(cfg.Settings) {
		if err := a.applyFirewallLocked(); err != nil {
			log.Printf("смена DNS: прозрачный режим: %v", err)
		}
	}
	log.Printf("ядро %s: новый DNS применён без остановки", name)
	a.kickAuto()
}

// tryBetterDNS возвращается к более приоритетному DNS, если тот снова отвечает.
func (a *App) tryBetterDNS(ctx context.Context, gen uint64) {
	if !dnsAuto(a.Store.Get().Settings) {
		return
	}
	cur := int(a.dnsIdx.Load())
	if cur == 0 {
		return
	}
	if i := firstWorkingDNS(ctx, 0, cur); i >= 0 {
		a.setDNS(i, "снова отвечает")
		a.reloadIfGen(gen)
	}
}
