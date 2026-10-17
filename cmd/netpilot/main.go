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
	go a.WatchAuto(watchCtx) // автоматические группы: проверка серверов по одному
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
		log.Print("пароль не задан: откройте интерфейс и придумайте его при первом входе")
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	// Важно остановить ядро: иначе на Windows останется выставленный системный прокси.
	_ = a.Stop()
	a.StopRouter()
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
