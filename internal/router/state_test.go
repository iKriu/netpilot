package router

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"netpilot/internal/model"
)

func TestUndoPlan(t *testing.T) {
	del := func(addr, dev string) []string { return []string{"ip", "addr", "del", addr, "dev", dev} }
	flush := func(dev string) []string { return []string{"ip", "-4", "addr", "flush", "dev", dev} }
	lan := model.LAN{Interface: "eth0", Address: "192.168.10.1/24"}
	cases := []struct {
		name string
		s    netState
		keep model.LAN
		cmds [][]string
		left netState
	}{
		{name: "ничего не делали", cmds: nil},
		{
			name: "тот же адрес на том же порту остаётся, запись тоже",
			s:    netState{LANAddr: "192.168.10.1/24", LANDev: "eth0"}, keep: lan,
			left: netState{LANAddr: "192.168.10.1/24", LANDev: "eth0"},
		},
		{
			// Раньше адрес сравнивался без порта и оставался на старом порту навсегда.
			name: "тот же адрес, но порт сменился — снимаем со старого",
			s:    netState{LANAddr: "192.168.10.1/24", LANDev: "eth2"}, keep: lan,
			cmds: [][]string{del("192.168.10.1/24", "eth2")},
		},
		{
			name: "выключение снимает всё записанное",
			s:    netState{LANAddr: "192.168.10.1/24", LANDev: "eth0", WANAddr: "203.0.113.10/24", WANDev: "eth1"},
			cmds: [][]string{del("203.0.113.10/24", "eth1"), del("192.168.10.1/24", "eth0")},
		},
		{
			// Сбой Apply после запуска udhcpc (например, не стартовал dnsmasq):
			// флаг «включено» не стоит, но адреса провайдера всё равно снимаются.
			name: "сбой после запуска udhcpc",
			s:    netState{LANAddr: "192.168.10.1/24", LANDev: "eth0", WANFlush: "eth1"},
			cmds: [][]string{flush("eth1"), del("192.168.10.1/24", "eth0")},
		},
		{
			name: "адрес был чужим (не записан) — не трогаем",
			s:    netState{WANAddr: "203.0.113.10/24", WANDev: "eth1"}, keep: lan,
			cmds: [][]string{del("203.0.113.10/24", "eth1")},
		},
	}
	for _, c := range cases {
		cmds, left := undoPlan(c.s, c.keep)
		if !reflect.DeepEqual(cmds, c.cmds) || left != c.left {
			t.Errorf("%s:\nкоманды %q\nосталось %+v", c.name, cmds, left)
		}
	}
}

// fakeProc — процесс, который «умирает» по команде теста.
type fakeProc struct {
	mu      sync.Mutex
	running bool
	starts  int
	stops   int
	fail    int // столько ближайших запусков завершится ошибкой
	args    []string
}

func (f *fakeProc) Start(bin string, args []string, dir, logPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.running {
		return errors.New("ядро уже запущено")
	}
	f.starts++
	f.args = args
	if f.fail > 0 {
		f.fail--
		return errors.New("не запустился")
	}
	f.running = true
	return nil
}

func (f *fakeProc) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.running = false
	return nil
}

func (f *fakeProc) Running() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}

func (f *fakeProc) kill() {
	f.mu.Lock()
	f.running = false
	f.mu.Unlock()
}

func (f *fakeProc) snapshot() (starts, stops int, running bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts, f.stops, f.running
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("не дождались: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestSupervise(t *testing.T) {
	dns := &fakeProc{running: true}
	wan := &fakeProc{running: true}
	cfg := superviseCfg{every: 2 * time.Millisecond, stable: time.Hour, delays: []time.Duration{10 * time.Millisecond, 40 * time.Millisecond}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	args := []string{"--interface=eth0"}
	go func() {
		defer close(done)
		supervise(ctx, []helper{{name: "dnsmasq", p: dns, bin: "dnsmasq", args: args}, {name: "udhcpc", p: wan, bin: "udhcpc"}}, cfg)
	}()

	// Живой процесс не трогаем.
	time.Sleep(20 * time.Millisecond)
	if s, _, _ := dns.snapshot(); s != 0 {
		t.Fatal("живой процесс перезапущен")
	}

	// Упал — перезапуск с теми же аргументами, Stop перед Start.
	dns.kill()
	waitFor(t, "перезапуск dnsmasq", func() bool { _, _, r := dns.snapshot(); return r })
	if s, st, _ := dns.snapshot(); s != 1 || st != 1 || !reflect.DeepEqual(dns.args, args) {
		t.Errorf("starts=%d stops=%d args=%v", s, st, dns.args)
	}
	if s, _, _ := wan.snapshot(); s != 0 {
		t.Error("второй процесс не должен перезапускаться")
	}

	// Не запускается — пробуем снова, с растущей паузой.
	wan.mu.Lock()
	wan.fail = 2
	wan.mu.Unlock()
	wan.kill()
	t0 := time.Now()
	waitFor(t, "перезапуск udhcpc после неудач", func() bool { _, _, r := wan.snapshot(); return r })
	if s, _, _ := wan.snapshot(); s != 3 {
		t.Errorf("попыток запуска %d, ожидалось 3", s)
	}
	if el := time.Since(t0); el < 10*time.Millisecond+40*time.Millisecond+40*time.Millisecond {
		t.Errorf("паузы между попытками не соблюдены: %v", el)
	}

	// После отмены — ни одного перезапуска.
	cancel()
	<-done
	dns.kill()
	before, _, _ := dns.snapshot()
	time.Sleep(30 * time.Millisecond)
	if after, _, _ := dns.snapshot(); after != before {
		t.Error("перезапуск после остановки присмотра")
	}
}

func TestSuperviseDelay(t *testing.T) {
	c := defaultSupervise
	var got []time.Duration
	for i := 0; i < 7; i++ {
		got = append(got, c.delay(i))
	}
	want := []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, time.Minute, time.Minute}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v", got)
	}
}

func TestHostsFile(t *testing.T) {
	text, skipped := hostsFile([]Lease{
		{MAC: "AA:BB:CC:DD:EE:01", IP: "192.168.10.50", Name: "TV"},
		{MAC: "aa:bb:cc:dd:ee:02\ndhcp-script=/tmp/x", IP: "192.168.10.51"},
		{MAC: "aa:bb:cc:dd:ee:03", IP: "192.168.10.52\nserver=1.2.3.4"},
		{MAC: "aa:bb:cc:dd:ee:04", IP: "fe80::1"},
		{MAC: "aa:bb:cc:dd:ee:05", IP: "192.168.10.55", Name: "a\nb,c"},
		{MAC: "", IP: "192.168.10.56"}, // незакреплённое — молча
	})
	want := "aa:bb:cc:dd:ee:01,192.168.10.50,TV\naa:bb:cc:dd:ee:05,192.168.10.55,abc\n"
	if text != want || skipped != 3 {
		t.Errorf("пропущено %d:\n%s", skipped, text)
	}
}

func TestInjection(t *testing.T) {
	for _, bad := range []string{"p\nplugin /tmp/evil.so", "p\rconnect x", "p\x00", "p\x7f", "p x", "p\xffx"} {
		if _, err := pppQuote(bad); err == nil {
			t.Errorf("pppQuote(%q) принял", bad)
		}
		if _, err := uciQuote(bad); err == nil {
			t.Errorf("uciQuote(%q) принял", bad)
		}
		w := model.WAN{Interface: "eth1", Username: "u", Password: bad}
		if o, err := pppOptionsFor("/usr/lib/pppd/2.5.1/pppoe.so", w); err == nil || o != "" {
			t.Errorf("pppOptionsFor с паролем %q: %v\n%s", bad, err, o)
		}
		r := good()
		r.WAN.Service = bad
		if s, err := uciRouter(r); err == nil || s != "" {
			t.Errorf("uciRouter с сервисом %q: %v", bad, err)
		}
		r = good()
		r.WAN.Password = bad
		if err := Validate(r, nil); err == nil || !strings.Contains(err.Error(), "пароль") {
			t.Errorf("Validate с паролем %q: %v", bad, err)
		}
	}
	if _, err := pppOptionsFor("/usr/lib/pppd/2.5.1/pppoe.so", model.WAN{Interface: "eth1\nplugin x", Username: "u"}); err == nil {
		t.Error("pppOptionsFor принял имя порта с переводом строки")
	}
	r := good()
	r.WAN.DNS = []string{"1.1.1.1\nnameserver 6.6.6.6"}
	if _, err := uciRouter(r); err == nil {
		t.Error("uciRouter принял DNS с переводом строки")
	}
	// Обычные кавычки и пробелы в пароле по-прежнему допустимы.
	if q, err := pppQuote(`a "b" \c`); err != nil || q != `"a \"b\" \\c"` {
		t.Errorf("%q %v", q, err)
	}
}

func TestValidateInterfaceNames(t *testing.T) {
	for _, name := range []string{"eth0", "enp3s0", "eth0.100", "wan@eth0", "br-lan", "eth0:1", "abcdefghijklmno"} {
		if !validIface(name) {
			t.Errorf("%q отвергнуто", name)
		}
	}
	for _, name := range []string{"", "abcdefghijklmnop", "eth 0", "eth0\n", "eth/0", "-x", ".", "..", "порт"} {
		if validIface(name) {
			t.Errorf("%q принято", name)
		}
	}
	// Строгая проверка имени работает и без списка портов.
	for _, mutate := range []func(r *model.Router){
		func(r *model.Router) { r.WAN.Interface = "eth1\nplugin x" },
		func(r *model.Router) { r.LAN.Interface = "eth 0" },
		func(r *model.Router) { r.LAN.Interface = "abcdefghijklmnop" },
		func(r *model.Router) { r.WAN.DNS = []string{"1.1.1.1\n"} },
		func(r *model.Router) { r.LAN.RangeEnd = "192.168.10.250\r" },
	} {
		r := good()
		mutate(&r)
		if err := Validate(r, nil); err == nil {
			t.Errorf("%+v: ожидалась ошибка", r)
		}
	}
}
