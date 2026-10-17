package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"netpilot/internal/links"
	"netpilot/internal/model"
	"netpilot/internal/store"
)

// UpdateSubscription скачивает подписку и заменяет её узлы.
// ID узлов с тем же адресом и именем сохраняются, чтобы не ломать правила.
// changed — изменился ли набор узлов (иначе ядро перезапускать незачем).
func (a *App) UpdateSubscription(ctx context.Context, id string) (count int, changed bool, err error) {
	var sub *model.Subscription
	for _, s := range a.Store.Get().Subscriptions {
		if s.ID == id {
			sub = &s
			break
		}
	}
	if sub == nil {
		return 0, false, errors.New("подписка не найдена")
	}

	body, ferr := a.download(ctx, sub.URL, 8<<20)
	var nodes []model.Node
	if ferr == nil {
		nodes, _ = links.ParseMany(string(body))
		if len(nodes) == 0 {
			ferr = errors.New("в подписке не найдено ни одного сервера")
		}
	}

	err = a.Store.Update(func(c *model.Config) error {
		for i := range c.Subscriptions {
			if c.Subscriptions[i].ID != id {
				continue
			}
			if ferr != nil {
				// Время обновления не трогаем: подписка остаётся «к обновлению»,
				// и следующая проверка (раз в 10 минут) попробует снова.
				c.Subscriptions[i].LastError = ferr.Error()
				return nil
			}
			c.Subscriptions[i].UpdatedAt = time.Now().Unix()
			c.Subscriptions[i].LastError = ""
		}
		if ferr != nil {
			return nil
		}
		// Серверы с одинаковыми именем и адресом бывают (разные порты или
		// протоколы), поэтому прежние id разбираются по очереди: каждый id
		// достаётся только одному серверу, иначе в ядре совпали бы теги.
		old := map[string][]model.Node{}
		oldCount := 0
		kept := c.Nodes[:0]
		for _, n := range c.Nodes {
			if n.SubscriptionID == id {
				k := n.Name + "|" + n.Server
				old[k] = append(old[k], n)
				oldCount++
			} else {
				kept = append(kept, n)
			}
		}
		c.Nodes = kept
		changed = oldCount != len(nodes)
		for _, n := range nodes {
			n.SubscriptionID = id
			k := n.Name + "|" + n.Server
			prevs := old[k]
			ok := len(prevs) > 0
			var prev model.Node
			if ok {
				prev, old[k] = prevs[0], prevs[1:]
				n.ID = prev.ID
				if !sameNode(prev, n) {
					changed = true
				}
			} else {
				n.ID = store.NewID()
				changed = true
			}
			c.Nodes = append(c.Nodes, n)
		}
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	if ferr != nil {
		return 0, false, ferr
	}
	if changed {
		a.subsChanged()
	}
	return len(nodes), changed, nil
}

func sameNode(a, b model.Node) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

// WatchSubscriptions обновляет подписки по расписанию. Если узлы изменились,
// а ядро работает с актуальной конфигурацией, новая применяется сразу:
// роутер должен обновляться сам, без участия человека.
func (a *App) WatchSubscriptions(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		a.updateDueSubscriptions(ctx)
		a.refreshLists(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (a *App) updateDueSubscriptions(ctx context.Context) {
	now := time.Now()
	anyChanged := false
	for _, s := range a.Store.Get().Subscriptions {
		if s.Interval <= 0 || now.Sub(time.Unix(s.UpdatedAt, 0)) < time.Duration(s.Interval)*time.Hour {
			continue
		}
		n, changed, err := a.UpdateSubscription(ctx, s.ID)
		if err != nil {
			log.Printf("подписка «%s»: %v", s.Name, err)
			continue
		}
		log.Printf("подписка «%s» обновлена: серверов %d%s", s.Name, n, map[bool]string{true: ", есть изменения", false: ""}[changed])
		anyChanged = anyChanged || changed
	}
	if !anyChanged {
		return
	}
	a.mu.Lock()
	running, onlySubs := a.active != nil, a.dirty && a.dirtyBySubs
	a.mu.Unlock()
	// Применяем, только если других неприменённых изменений нет: иначе
	// можно применить за пользователя то, что он ещё не закончил настраивать.
	if running && onlySubs {
		if err := a.Apply(); err != nil {
			log.Printf("применение после обновления подписок: %v", err)
		}
	}
}

// download скачивает файл: сначала напрямую, при ошибке — через свой прокси,
// если ядро запущено (сайт провайдера или GitHub могут быть недоступны напрямую).
func (a *App) download(ctx context.Context, u string, limit int64) ([]byte, error) {
	body, err := fetch(ctx, u, nil, limit)
	if err == nil {
		return body, nil
	}
	a.mu.Lock()
	running := a.active != nil
	a.mu.Unlock()
	if !running {
		return nil, err
	}
	s := a.Store.Get().Settings
	host := s.ListenAddr
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	proxy := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(s.MixedPort))}
	body, perr := fetch(ctx, u, proxy, limit)
	if perr != nil {
		return nil, fmt.Errorf("напрямую: %v; через прокси: %v", err, perr)
	}
	return body, nil
}

func fetch(ctx context.Context, u string, proxy *url.URL, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NetPilot/"+Version)
	client := http.DefaultClient
	if proxy != nil {
		client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("сервер ответил %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if int64(len(body)) > limit {
		return nil, errors.New("файл слишком большой")
	}
	return body, err
}
