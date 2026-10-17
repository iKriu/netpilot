package scan

import (
	"context"
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeOpener открывает обычный UDP-сокет на 127.0.0.1 вместо порта 67
// и запоминает все открытые сокеты.
type fakeOpener struct {
	mu    sync.Mutex
	conns []*trackedConn
}

type trackedConn struct {
	net.PacketConn
	closed atomic.Bool
}

func (c *trackedConn) Close() error { c.closed.Store(true); return c.PacketConn.Close() }

func (f *fakeOpener) open() (net.PacketConn, error) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	c := &trackedConn{PacketConn: pc}
	f.mu.Lock()
	f.conns = append(f.conns, c)
	f.mu.Unlock()
	return c, nil
}

func (f *fakeOpener) snapshot() []*trackedConn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*trackedConn(nil), f.conns...)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("не дождались: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func dhcpPacket(mac []byte, host string) []byte {
	b := make([]byte, 240)
	b[0], b[1], b[2] = 1, 1, 6
	copy(b[28:], mac)
	binary.BigEndian.PutUint32(b[236:], 0x63825363)
	b = append(b, 12, byte(len(host)))
	b = append(b, host...)
	return append(b, 255)
}

func TestDHCPListenerFollowsAllowed(t *testing.T) {
	var allowed atomic.Bool
	allowed.Store(true)
	f := &fakeOpener{}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		runDHCPListener(ctx, allowed.Load, f.open, 10*time.Millisecond)
		close(stopped)
	}()

	waitFor(t, "сокет открыт", func() bool { return len(f.snapshot()) == 1 })
	first := f.snapshot()[0]

	// Запрос, пришедший в открытый сокет, даёт имя устройства.
	out, err := net.Dial("udp4", first.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = out.Write(dhcpPacket([]byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0x01}, "Pixel-8"))
	out.Close()
	waitFor(t, "имя из DHCP", func() bool { return dhcpNames()["02:aa:bb:cc:dd:01"] == "Pixel-8" })

	// Режим роутера включили — порт освобождается.
	allowed.Store(false)
	waitFor(t, "сокет закрыт", first.closed.Load)
	time.Sleep(50 * time.Millisecond)
	if n := len(f.snapshot()); n != 1 {
		t.Fatalf("пока запрещено, открыто сокетов: %d", n)
	}

	// Выключили — слушаем снова.
	allowed.Store(true)
	waitFor(t, "сокет открыт заново", func() bool { return len(f.snapshot()) == 2 })

	cancel()
	<-stopped
	for i, c := range f.snapshot() {
		if !c.closed.Load() {
			t.Fatalf("сокет %d не закрыт после остановки", i)
		}
	}
}

func TestDHCPListenerRetriesOpen(t *testing.T) {
	var fails atomic.Int32
	f := &fakeOpener{}
	open := func() (net.PacketConn, error) {
		if fails.Add(1) <= 2 {
			return nil, net.ErrClosed // порт пока занят
		}
		return f.open()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runDHCPListener(ctx, nil, open, 10*time.Millisecond)
	waitFor(t, "сокет открыт после неудач", func() bool { return len(f.snapshot()) == 1 })
}

// ReleaseDHCPPort закрывает сокет сразу (не ждёт следующей проверки allowed)
// и не даёт открыть его заново, пока длится удержание.
func TestReleaseDHCPPort(t *testing.T) {
	// Слушатели прошлых тестов ещё могут доживать и забрать сигнал себе.
	waitFor(t, "прежние слушатели остановились", func() bool { return dhcpRunning.Load() == 0 })
	f := &fakeOpener{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runDHCPListener(ctx, nil, f.open, time.Hour) // allowed() сам ничего не закроет
	waitFor(t, "сокет открыт", func() bool { return len(f.snapshot()) == 1 })

	ReleaseDHCPPort(200 * time.Millisecond)
	if !f.snapshot()[0].closed.Load() {
		t.Fatal("после ReleaseDHCPPort сокет должен быть уже закрыт")
	}
	dhcpHoldUntil.Store(0) // удержание вышло
	ReleaseDHCPPort(0)     // будим цикл, чтобы он открыл порт заново
	waitFor(t, "сокет открыт после удержания", func() bool { return len(f.snapshot()) == 2 })
}
