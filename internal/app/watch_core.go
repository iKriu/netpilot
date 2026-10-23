package app

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"netpilot/internal/atomicfile"
	"netpilot/internal/core"
)

// Слежение за ядром в отдельном процессе (Xray, mihomo, Hiddify). Если ядро
// упало, а NetPilot этого не заметил, статус врёт «работает», а в прозрачном
// режиме перехват отправляет весь трафик сети в несуществующий процесс —
// интернет пропадает у всех устройств.
//
// Каждый запуск ядра получает номер (coreGen). Остановка, перезапуск и
// применение настроек меняют номер, поэтому наблюдатель старого экземпляра,
// проснувшись, видит, что завершение было ожидаемым, и ничего не делает.

// restartDelays — паузы перед повторными попытками запуска.
var restartDelays = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute}

const (
	restartWindow   = 10 * time.Minute // сбои старше не считаются
	restartMaxFails = 5                // столько сбоев за окно — перестаём перезапускать
)

type restartState struct {
	pending bool        // ждём повторного запуска
	core    string      // какое ядро перезапускаем
	fails   []time.Time // недавние падения и неудачные попытки запуска
}

// watchLocked начинает следить за только что запущенным ядром.
func (a *App) watchLocked(c core.Core) {
	ex, ok := c.(core.Exiter)
	if !ok {
		return // встроенное ядро работает в нашем процессе
	}
	gen, exited := a.coreGen, ex.Exited()
	go func() {
		<-exited
		a.coreExited(ex, gen)
	}()
}

// coreExited — процесс ядра завершился. Если это не наша остановка — падение.
func (a *App) coreExited(ex core.Exiter, gen uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.coreGen != gen || a.active == nil {
		return // остановлено или заменено нами
	}
	name, reason := a.active.Name(), ex.ExitReason()
	log.Printf("ядро %s неожиданно завершилось: %s", name, reason)
	a.coreFailedLocked(name, "неожиданно завершилось: "+reason)
}

// coreFailedLocked — ядро упало или перестало пропускать трафик: остановить
// его, применить политику при сбое и назначить перезапуск.
func (a *App) coreFailedLocked(name, what string) {
	a.keepCrashLog()
	_ = a.active.Stop() // освобождаем состояние процесса и адрес API
	// Статус больше не «работает», пока ядро не запустится снова.
	a.active, a.dirty, a.dirtyBySubs = nil, false, false
	a.coreGen++
	if !a.failClosed() {
		// Трафик устройств больше некому принимать — пусть идёт напрямую.
		a.removeFirewallLocked()
	}
	a.scheduleRestartLocked(name, what)
}

// failClosed — при падении ядра держать перехват (интернет блокирован), а не
// пускать устройства напрямую в обход прокси.
func (a *App) failClosed() bool { return a.Store.Get().Settings.CoreFailure == "closed" }

// scheduleRestartLocked назначает повторный запуск с нарастающей паузой или,
// если ядро падает раз за разом, сдаётся и оставляет его остановленным.
func (a *App) scheduleRestartLocked(name, what string) {
	r := &a.restart
	now := time.Now()
	recent := r.fails[:0]
	for _, t := range r.fails {
		if now.Sub(t) < restartWindow {
			recent = append(recent, t)
		}
	}
	r.fails = append(recent, now)

	tp := transparent(a.Store.Get().Settings)
	if len(r.fails) >= restartMaxFails {
		r.pending = false
		policy := ""
		switch {
		case a.fwOn:
			policy = " Интернет устройств заблокирован (так задано в настройках) — остановите шлюз, чтобы пустить их напрямую."
		case tp:
			policy = " Устройства идут в интернет напрямую."
		}
		a.lastErr = fmt.Sprintf("ядро %s %s. Оно падает раз за разом (%d раз за %d минут) — перезапуск прекращён, посмотрите журнал ядра и запустите шлюз вручную.%s",
			name, what, len(r.fails), int(restartWindow.Minutes()), policy)
		log.Printf("ядро %s: перезапуск прекращён после %d сбоев", name, len(r.fails))
		return
	}
	policy := ""
	switch {
	case a.fwOn:
		policy = ", пока интернет устройств заблокирован (так задано в настройках)"
	case tp:
		policy = ", пока устройства идут в интернет напрямую"
	}
	delay := restartDelays[min(len(r.fails), len(restartDelays))-1]
	r.pending, r.core = true, name
	a.lastErr = fmt.Sprintf("ядро %s %s. Перезапуск через %d с%s", name, what, int(delay.Seconds()), policy)
	gen := a.coreGen
	time.AfterFunc(delay, func() { a.tryRestart(gen) })
}

// tryRestart — повторный запуск после падения. Ничего не делает, если за это
// время шлюз остановили, запустили или применили настройки вручную.
func (a *App) tryRestart(gen uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.coreGen != gen || a.active != nil || !a.restart.pending {
		return
	}
	name := a.restart.core
	if err := a.startLocked(); err != nil {
		log.Printf("перезапуск ядра %s: %v", name, err)
		a.scheduleRestartLocked(name, "не перезапустилось: "+err.Error())
		return
	}
	log.Printf("ядро %s перезапущено после сбоя", a.active.Name())
}

// crashLogMax — сколько последних байт журнала хранить в core.crash.log.
const crashLogMax = 1 << 20

// keepCrashLog сохраняет хвост журнала ядра в момент сбоя в core.crash.log.
// Обычная ротация (core.log → core.log.1) при каждом запуске не спасает:
// после сбоя ядро перезапускается по нескольку раз, и журнал с причиной
// уходит за пределы одной сохранённой копии.
func (a *App) keepCrashLog() {
	path := filepath.Join(a.DataDir, "core.log")
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return
	}
	if st.Size() > crashLogMax {
		_, _ = f.Seek(st.Size()-crashLogMax, 0)
	}
	data := make([]byte, min(st.Size(), crashLogMax))
	n, _ := io.ReadFull(f, data)
	if err := atomicfile.Write(filepath.Join(a.DataDir, "core.crash.log"), data[:n], 0o644); err != nil {
		log.Printf("журнал сбоя ядра: %v", err)
	}
}
