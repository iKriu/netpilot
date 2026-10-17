package router

import (
	"context"
	"log"
	"time"

	"netpilot/internal/model"
)

// netState — что Apply действительно сделал с портами. Записи ставятся по
// мере успеха шагов, а teardown откатывает ровно их: ни конфигурация, ни
// флаг «включено» тут не участвуют — при сбое посреди Apply они ещё старые.
type netState struct {
	LANAddr, LANDev string // адрес, который мы добавили на LAN-порт (чужой не записываем)
	WANAddr, WANDev string // статический адрес на порту провайдера
	WANFlush        string // порт, где адреса ставил udhcpc: снимаем все IPv4
}

// undoPlan — команды отката и что останется после него. keep — локальная сеть
// новой конфигурации: её адрес на том же порту не снимаем (иначе пропадёт
// доступ к панели на секунду), но и запись о нём не теряем — чтобы убрать
// его при выключении. Тот же адрес на другом порту снимается.
func undoPlan(s netState, keep model.LAN) (cmds [][]string, left netState) {
	if s.WANAddr != "" {
		cmds = append(cmds, []string{"ip", "addr", "del", s.WANAddr, "dev", s.WANDev})
	}
	if s.WANFlush != "" {
		cmds = append(cmds, []string{"ip", "-4", "addr", "flush", "dev", s.WANFlush})
	}
	if s.LANAddr != "" {
		if s.LANAddr == keep.Address && s.LANDev == keep.Interface {
			left.LANAddr, left.LANDev = s.LANAddr, s.LANDev
		} else {
			cmds = append(cmds, []string{"ip", "addr", "del", s.LANAddr, "dev", s.LANDev})
		}
	}
	return cmds, left
}

// ---------- присмотр за dnsmasq, udhcpc и pppd ----------

// proc — то, что нужно присмотру от процесса (external.Process).
type proc interface {
	Start(bin string, args []string, dir, logPath string) error
	Stop() error
	Running() bool
}

// helper — вспомогательный процесс и всё, чтобы перезапустить его так же.
type helper struct {
	name string
	p    proc
	bin  string
	args []string
	dir  string
	log  string
}

type superviseCfg struct {
	every  time.Duration   // как часто проверять
	stable time.Duration   // столько проработал после перезапуска — счётчик неудач сбрасывается
	delays []time.Duration // паузы перед перезапусками, последняя — потолок
}

var defaultSupervise = superviseCfg{
	every:  3 * time.Second,
	stable: 2 * time.Minute,
	delays: []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second},
}

func (c superviseCfg) delay(attempt int) time.Duration {
	if attempt >= len(c.delays) {
		attempt = len(c.delays) - 1
	}
	return c.delays[attempt]
}

// supervise перезапускает упавшие процессы, пока не отменят ctx. Блокировку
// бэкенда не берёт: teardown держит её, пока ждёт завершения присмотра.
func supervise(ctx context.Context, hs []helper, c superviseCfg) {
	type st struct {
		attempt   int
		retryAt   time.Time
		restarted time.Time
	}
	state := make([]st, len(hs))
	t := time.NewTicker(c.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := time.Now()
		for i, h := range hs {
			s := &state[i]
			if h.p.Running() {
				if s.attempt > 0 && now.Sub(s.restarted) >= c.stable {
					s.attempt = 0
				}
				continue
			}
			if s.retryAt.IsZero() {
				d := c.delay(s.attempt)
				s.retryAt = now.Add(d)
				log.Printf("режим роутера: %s завершился, перезапуск через %v: %s", h.name, d, tailFile(h.log, 300))
				continue
			}
			if now.Before(s.retryAt) || ctx.Err() != nil {
				continue
			}
			// Stop очищает упавший процесс: иначе Start ответит «ядро уже запущено».
			_ = h.p.Stop()
			err := h.p.Start(h.bin, h.args, h.dir, h.log)
			s.attempt++
			s.retryAt, s.restarted = time.Time{}, now
			if err != nil {
				log.Printf("режим роутера: не удалось перезапустить %s: %v", h.name, err)
			} else {
				log.Printf("режим роутера: %s перезапущен", h.name)
			}
		}
	}
}
