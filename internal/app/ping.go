package app

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"

	"netpilot/internal/model"
)

// Способы проверки задержки серверов (настройка PingMethod).
const (
	PingAuto = "auto" // через ядро, если шлюз запущен, иначе TCP
	PingHTTP = "http" // настоящий запрос через сервер (нужен запущенный шлюз)
	PingTCP  = "tcp"  // время подключения к порту сервера
	PingICMP = "icmp" // ping адреса сервера
)

const pingTimeout = 4 * time.Second

// icmpPing — эхо-запрос ICMP к адресу сервера. Сначала без прав root
// (Linux, macOS: «ping-сокет»), затем обычным сокетом (root, администратор),
// и в последнюю очередь — системной командой ping.
func icmpPing(ctx context.Context, n model.Node) Latency {
	res := Latency{ID: n.ID, Ms: -1, Method: PingICMP}
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", n.Server)
	if err != nil || len(ips) == 0 {
		res.Error = "адрес сервера не найден"
		return res
	}
	ip := ips[0]
	if ms, err := icmpSocket(ctx, ip); err == nil {
		res.Ms = ms
		return res
	} else if !errors.Is(err, errNoICMPSocket) {
		res.Error = err.Error()
		return res
	}
	ms, err := systemPing(ctx, ip.String())
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Ms = ms
	return res
}

var errNoICMPSocket = errors.New("нет доступа к ICMP")

func icmpSocket(ctx context.Context, ip net.IP) (int, error) {
	// «udp4» — ICMP без прав root, «ip4:icmp» — с правами.
	var conn *icmp.PacketConn
	unpriv := false
	if runtime.GOOS != "windows" {
		if c, err := icmp.ListenPacket("udp4", "0.0.0.0"); err == nil {
			conn, unpriv = c, true
		}
	}
	if conn == nil {
		c, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
		if err != nil {
			return 0, errNoICMPSocket
		}
		conn = c
	}
	defer conn.Close()

	id, seq := rand.IntN(0xffff), 1
	msg := icmp.Message{Type: ipv4.ICMPTypeEcho, Body: &icmp.Echo{ID: id, Seq: seq, Data: []byte("netpilot")}}
	b, _ := msg.Marshal(nil)
	var dst net.Addr = &net.IPAddr{IP: ip}
	if unpriv {
		dst = &net.UDPAddr{IP: ip}
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	start := time.Now()
	if _, err := conn.WriteTo(b, dst); err != nil {
		return 0, fmt.Errorf("отправка: %w", err)
	}
	buf := make([]byte, 1500)
	for {
		nr, peer, err := conn.ReadFrom(buf)
		if err != nil {
			return 0, errors.New("не отвечает на ping")
		}
		rm, err := icmp.ParseMessage(1, buf[:nr])
		if err != nil || rm.Type != ipv4.ICMPTypeEchoReply {
			continue
		}
		echo, ok := rm.Body.(*icmp.Echo)
		// Ядро меняет ID у «ping-сокета», поэтому без прав сверяем отправителя.
		if !ok || echo.Seq != seq || (!unpriv && echo.ID != id) || !samePeer(peer, ip) {
			continue
		}
		return max(1, int(time.Since(start).Milliseconds())), nil
	}
}

func samePeer(a net.Addr, ip net.IP) bool {
	switch v := a.(type) {
	case *net.IPAddr:
		return v.IP.Equal(ip)
	case *net.UDPAddr:
		return v.IP.Equal(ip)
	}
	return false
}

// Время из вывода системного ping: «time=12.3 ms», «время=12мс», «time<1ms».
var pingTime = regexp.MustCompile(`[=<]\s*([0-9]+(?:[.,][0-9]+)?)\s*(?:ms|мс)`)

func systemPing(ctx context.Context, ip string) (int, error) {
	args := []string{"-c", "1", "-W", "3", ip}
	if runtime.GOOS == "windows" {
		args = []string{"-n", "1", "-w", "3000", ip}
	}
	out, err := exec.CommandContext(ctx, "ping", args...).CombinedOutput()
	if m := pingTime.FindSubmatch(out); m != nil {
		v, _ := strconv.ParseFloat(string(m[1]), 64)
		return max(1, int(v+0.5)), nil
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || os.IsNotExist(err) {
			return 0, errors.New("ICMP недоступен: нет прав и команды ping")
		}
	}
	return 0, errors.New("не отвечает на ping")
}
