package scan

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Имена из DHCP-запросов. Подключаясь к сети, устройство рассылает всем
// DHCP-запрос и в нём называет себя (опция 12: «Galaxy-S23», «iPhone»).
// Адреса раздаёт роутер, но запрос широковещательный — его видит и NetPilot.
var dhcpSeen sync.Map // MAC → имя

// dhcpCheckEvery — как часто проверять, можно ли сейчас слушать порт 67.
const dhcpCheckEvery = 3 * time.Second

// ListenDHCP слушает DHCP-запросы в сети, ничего не отвечая, пока allowed()
// разрешает. Порт 67 нужен DHCP-серверу на этой же машине: на OpenWrt и в
// режиме роутера адреса раздаёт dnsmasq, и имена берутся из его файла аренд
// (ExtraLeaseFiles). Режим роутера включается на ходу, поэтому allowed()
// проверяется периодически и сокет закрывается или открывается заново.
func ListenDHCP(ctx context.Context, allowed func() bool) {
	if runtime.GOOS != "linux" || fileExists("/etc/openwrt_release") {
		return
	}
	runDHCPListener(ctx, allowed, openDHCP, dhcpCheckEvery)
}

// Порт открывается без SO_REUSEADDR: если на машине уже работает свой
// DHCP-сервер (Pi-hole, dnsmasq), занять порт рядом с ним нельзя — и не нужно.
// С SO_REUSEADDR ядро Linux отдавало бы адресные запросы (продление аренды)
// только одному из сокетов, и часть их доставалась бы нам, а не серверу.
func openDHCP() (net.PacketConn, error) { return net.ListenPacket("udp4", "0.0.0.0:67") }

var (
	dhcpRunning   atomic.Int32             // сколько циклов слушателя запущено
	dhcpHoldUntil atomic.Int64             // до какого момента (unix ns) порт не открывать
	dhcpRelease   = make(chan chan struct{}) // просьба закрыть сокет сейчас
)

// ReleaseDHCPPort закрывает сокет слушателя и не открывает его hold: так
// dnsmasq режима роутера гарантированно получит порт 67, даже если режим
// включают на ходу, а проверка allowed() ещё не успела сработать.
func ReleaseDHCPPort(hold time.Duration) {
	dhcpHoldUntil.Store(time.Now().Add(hold).UnixNano())
	if dhcpRunning.Load() == 0 {
		return
	}
	ack := make(chan struct{})
	select {
	case dhcpRelease <- ack:
		<-ack
	case <-time.After(2 * time.Second): // цикл занят открытием — откроет уже с учётом hold
	}
}

// runDHCPListener — цикл управления сокетом; open подменяется в тестах.
func runDHCPListener(ctx context.Context, allowed func() bool, open func() (net.PacketConn, error), every time.Duration) {
	dhcpRunning.Add(1)
	defer dhcpRunning.Add(-1)
	var (
		pc   net.PacketConn
		done chan struct{}
	)
	closeConn := func() {
		if pc != nil {
			pc.Close()
			<-done // чтение завершилось — порт свободен
			pc = nil
		}
	}
	defer closeConn()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		held := time.Now().UnixNano() < dhcpHoldUntil.Load()
		if !held && (allowed == nil || allowed()) {
			if pc == nil {
				if c, err := open(); err == nil {
					pc, done = c, make(chan struct{})
					go readDHCP(c, done)
				}
			}
		} else {
			closeConn()
		}
		select {
		case <-ctx.Done():
			return
		case ack := <-dhcpRelease:
			closeConn()
			close(ack)
		case <-t.C:
		}
	}
}

func readDHCP(pc net.PacketConn, done chan struct{}) {
	defer close(done)
	buf := make([]byte, 1500)
	for {
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			time.Sleep(100 * time.Millisecond) // не крутиться впустую на повторяющейся ошибке
			continue
		}
		if mac, name := parseDHCPRequest(buf[:n]); mac != "" && name != "" {
			dhcpSeen.Store(mac, name)
		}
	}
}

// dhcpNames — имена, услышанные в DHCP-запросах: MAC → имя.
func dhcpNames() map[string]string {
	out := map[string]string{}
	dhcpSeen.Range(func(k, v any) bool { out[k.(string)] = v.(string); return true })
	return out
}

func parseDHCPRequest(b []byte) (mac, name string) {
	// BOOTREQUEST, Ethernet, MAC длиной 6, «волшебное число» DHCP.
	if len(b) < 240 || b[0] != 1 || b[1] != 1 || b[2] != 6 || binary.BigEndian.Uint32(b[236:]) != 0x63825363 {
		return "", ""
	}
	mac = net.HardwareAddr(b[28:34]).String()
	var host, fqdn string
	for i := 240; i < len(b); {
		code := b[i]
		if code == 0 {
			i++
			continue
		}
		if code == 255 || i+1 >= len(b) {
			break
		}
		l := int(b[i+1])
		if i+2+l > len(b) {
			break
		}
		v := b[i+2 : i+2+l]
		switch code {
		case 12: // имя узла
			host = string(v)
		case 81: // полное имя: флаги, 2 байта, затем имя (текстом или в формате DNS)
			if len(v) > 3 {
				if v[0]&0x04 != 0 {
					fqdn, _, _ = readName(append(append([]byte{}, v[3:]...), 0), 0)
				} else {
					fqdn = string(v[3:])
				}
			}
		}
		i += 2 + l
	}
	if host == "" {
		host, _, _ = strings.Cut(fqdn, ".")
	}
	return mac, cleanName(strings.TrimSpace(strings.Trim(host, "\x00")))
}
