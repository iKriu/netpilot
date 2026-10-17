package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"netpilot/internal/api"
	"netpilot/internal/app"
	"netpilot/internal/auth"
	"netpilot/internal/sysproxy"
	"netpilot/internal/update"
	"netpilot/web"
)

func main() {
	dataDir := flag.String("data", env("NETPILOT_DATA", "data"), "папка с настройками")
	listen := flag.String("listen", env("NETPILOT_LISTEN", "127.0.0.1:2017"), "адрес веб-интерфейса")
	password := flag.String("password", os.Getenv("NETPILOT_PASSWORD"), "начальный пароль веб-интерфейса (если ещё не задан)")
	scanNet := flag.Bool("scan", env("NETPILOT_SCAN", "1") != "0", "следить за устройствами в сети")
	showVersion := flag.Bool("version", false, "показать версию и выйти")
	flag.Parse()
	if *showVersion {
		fmt.Println(app.Version)
		return
	}

	// До всего остального: если прошлое обновление не запускается, здесь
	// произойдёт возврат к предыдущей версии.
	upd := update.New(*dataDir, app.Version)

	a, err := app.New(*dataDir)
	if err != nil {
		log.Fatalf("не удалось открыть настройки: %v", err)
	}
	// Прошлый запуск мог не успеть выключить системный прокси Windows —
	// тогда он указывает на мёртвый порт и интернет пропадает везде.
	ps := a.Store.Get().Settings
	if cleared, err := sysproxy.ClearStale(ps.ListenAddr, ps.MixedPort); err != nil {
		log.Printf("системный прокси: %v", err)
	} else if cleared {
		log.Print("выключен системный прокси, оставшийся от прошлого запуска")
	}
	// Сначала интернет (режим роутера), потом ядро прокси.
	a.StartRouter()
	if a.Store.Get().Settings.AutoStart {
		if err := a.Start(); err != nil {
			log.Printf("автозапуск ядра: %v", err)
		}
	}

	watchCtx, stopWatch := context.WithCancel(context.Background())
	defer stopWatch()
	if *scanNet {
		go a.WatchNetwork(watchCtx)
	}
	go a.WatchSubscriptions(watchCtx)
	go a.WatchTraffic(watchCtx)
	go a.WatchAuto(watchCtx)   // автоматические группы: проверка серверов по одному
	go a.WatchHealth(watchCtx) // ядро пропускает трафик: DNS отвечает
	go upd.Watch(watchCtx)

	static := web.Static()
	// Для разработки интерфейса: файлы берутся с диска, пересборка не нужна.
	if dir := os.Getenv("NETPILOT_WEB_DIR"); dir != "" {
		static = os.DirFS(dir)
	}
	au, err := auth.Open(*dataDir)
	if err != nil {
		log.Fatalf("настройки входа: %v", err)
	}
	if *password != "" && !au.Configured() {
		if _, err := au.Setup(*password); err != nil {
			log.Fatalf("начальный пароль: %v", err)
		}
		log.Print("задан начальный пароль из NETPILOT_PASSWORD")
	}
	if !au.Configured() {
		// Код нужен при первой настройке не с этого компьютера: иначе пароль
		// задал бы любой в сети, кто первым откроет страницу.
		code, err := au.NewSetupCode()
		if err != nil {
			log.Fatalf("код первой настройки: %v", err)
		}
		log.Print("пароль не задан: откройте интерфейс и придумайте его при первом входе")
		log.Print("==================================================")
		log.Printf("  КОД ПЕРВОЙ НАСТРОЙКИ: %s", code)
		log.Print("  нужен, если интерфейс открыт с другого устройства")
		log.Print("==================================================")
	}
	handler := api.New(a, au, upd, static)
	srv := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		log.Printf("NetPilot %s — интерфейс: http://%s", app.Version, *listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("веб-интерфейс: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	restart := false
	select {
	case <-sig:
		log.Print("завершение...")
	case <-upd.Restart():
		log.Print("перезапуск в новую версию...")
		restart = true
	}
	// Сначала ядро: оно снимает системный прокси Windows. Windows завершает
	// консольную программу примерно через 5 с после закрытия окна — если
	// раньше ждать веб-сервер, прокси останется указывать на мёртвый порт.
	_ = a.Shutdown()
	a.StopRouter()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	if restart {
		upd.Reexec()
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
