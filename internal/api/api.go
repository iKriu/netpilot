// Package api — REST API для веб-интерфейса.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"slices"
	"strings"
	"time"

	"netpilot/internal/app"
	"netpilot/internal/auth"
	"netpilot/internal/core"
	"netpilot/internal/model"
	"netpilot/internal/profiles"
	"netpilot/internal/store"
	"netpilot/internal/update"
)

type Server struct {
	app  *app.App
	auth *auth.Auth
	upd  *update.Updater
	mux  *http.ServeMux
}

func New(a *app.App, au *auth.Auth, upd *update.Updater, static fs.FS) http.Handler {
	s := &Server{app: a, auth: au, upd: upd, mux: http.NewServeMux()}
	m := s.mux
	s.registerAuth()
	s.registerUpdate()

	m.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		reply(w, a.Status(), nil)
	})
	// Статус берём после действия, а не до.
	for path, action := range map[string]func() error{"start": a.Start, "stop": a.Stop, "apply": a.Apply} {
		m.HandleFunc("POST /api/core/"+path, func(w http.ResponseWriter, r *http.Request) {
			err := action()
			reply(w, a.Status(), err)
		})
	}
	m.HandleFunc("GET /api/cores", func(w http.ResponseWriter, r *http.Request) {
		reply(w, a.CoreList(), nil)
	})
	m.HandleFunc("GET /api/core/preview", func(w http.ResponseWriter, r *http.Request) {
		name, reason, issues, data, err := a.Preview()
		reply(w, map[string]any{
			"core": name, "reason": reason, "issues": issues, "config": json.RawMessage(orNull(data)),
		}, err)
	})
	m.HandleFunc("GET /api/core/log", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]string{"log": a.CoreLog(64 << 10)}, nil)
	})
	m.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		reply(w, profiles.All(), nil)
	})

	m.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		reply(w, a.Store.Get().Settings, nil)
	})
	m.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var in model.Settings
		if !decode(w, r, &in) {
			return
		}
		if in.MixedPort <= 0 || in.MixedPort > 65535 {
			reply(w, nil, errors.New("неверный порт прокси"))
			return
		}
		err := a.Store.Update(func(c *model.Config) error { c.Settings = in; return nil })
		if err == nil {
			a.Changed()
		}
		reply(w, in, err)
	})

	collection(s, "nodes", func(c *model.Config) *[]model.Node { return &c.Nodes },
		func(n *model.Node) *string { return &n.ID }, nil, a.Changed)
	collection(s, "groups", func(c *model.Config) *[]model.Group { return &c.Groups },
		func(g *model.Group) *string { return &g.ID }, core.CheckGroup, a.Changed)
	collection(s, "rules", func(c *model.Config) *[]model.Rule { return &c.Rules },
		func(r *model.Rule) *string { return &r.ID }, nil, a.Changed)
	collection(s, "devices", func(c *model.Config) *[]model.Device { return &c.Devices },
		func(d *model.Device) *string { return &d.ID }, nil, a.DevicesChanged)
	collection(s, "subscriptions", func(c *model.Config) *[]model.Subscription { return &c.Subscriptions },
		func(sub *model.Subscription) *string { return &sub.ID }, nil, func() {}) // на маршрутизацию не влияют

	// Сервер в группе с ручным выбором.
	m.HandleFunc("POST /api/groups/{id}/select", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			NodeID string `json:"nodeId"`
		}
		if !decode(w, r, &in) {
			return
		}
		reply(w, nil, a.SelectInGroup(r.Context(), r.PathValue("id"), in.NodeID))
	})

	m.HandleFunc("POST /api/cores/{name}/install", func(w http.ResponseWriter, r *http.Request) {
		v, err := a.InstallCore(r.Context(), r.PathValue("name"))
		reply(w, map[string]string{"version": v}, err)
	})
	m.HandleFunc("DELETE /api/cores/{name}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, nil, a.UninstallCore(r.PathValue("name")))
	})
	// ---- списки (Re:filter) ----
	m.HandleFunc("GET /api/lists", func(w http.ResponseWriter, r *http.Request) {
		reply(w, a.Lists(), nil)
	})
	m.HandleFunc("POST /api/lists/update", func(w http.ResponseWriter, r *http.Request) {
		err := a.UpdateLists(r.Context())
		reply(w, a.Lists(), err)
	})

	// ---- режим роутера ----
	m.HandleFunc("GET /api/router", func(w http.ResponseWriter, r *http.Request) {
		reply(w, a.RouterView(r.Context()), nil)
	})
	m.HandleFunc("PUT /api/router", func(w http.ResponseWriter, r *http.Request) {
		var in model.Router
		if !decode(w, r, &in) {
			return
		}
		deadline, err := a.RouterApply(in)
		reply(w, map[string]int64{"deadline": deadline.Unix()}, err)
	})
	m.HandleFunc("POST /api/router/confirm", func(w http.ResponseWriter, r *http.Request) {
		reply(w, nil, a.RouterConfirm())
	})
	m.HandleFunc("POST /api/router/reconnect", func(w http.ResponseWriter, r *http.Request) {
		reply(w, nil, a.RouterReconnect())
	})
	m.HandleFunc("GET /api/router/log", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]string{"log": a.RouterLog()}, nil)
	})

	m.HandleFunc("GET /api/backup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="netpilot-backup-%s.json"`, time.Now().Format("2006-01-02")))
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(a.Backup())
	})
	m.HandleFunc("POST /api/restore", func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
		if err != nil {
			reply(w, nil, err)
			return
		}
		reply(w, nil, a.Restore(data))
	})
	m.HandleFunc("GET /api/transparent", func(w http.ResponseWriter, r *http.Request) {
		reply(w, a.Transparent(), nil)
	})
	m.HandleFunc("GET /api/connections", func(w http.ResponseWriter, r *http.Request) {
		reply(w, a.Connections(), nil)
	})
	m.HandleFunc("DELETE /api/connections", func(w http.ResponseWriter, r *http.Request) {
		reply(w, nil, a.CloseConnection(r.Context(), ""))
	})
	m.HandleFunc("DELETE /api/connections/{id}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, nil, a.CloseConnection(r.Context(), r.PathValue("id")))
	})
	m.HandleFunc("GET /api/network", func(w http.ResponseWriter, r *http.Request) {
		reply(w, a.Network(), nil)
	})
	m.HandleFunc("POST /api/network/scan", func(w http.ResponseWriter, r *http.Request) {
		rep, err := a.ScanNetwork(r.Context(), true)
		reply(w, rep, err)
	})
	// Массовые действия над устройствами: маршрут, закрепление.
	m.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		reply(w, a.Stats(), nil)
	})
	// Куда пойдёт сайт: ?host=parsec.app&source=192.168.0.32 (устройство — необязательно).
	m.HandleFunc("GET /api/route/explain", func(w http.ResponseWriter, r *http.Request) {
		host := r.URL.Query().Get("host")
		if strings.TrimSpace(host) == "" {
			reply(w, nil, errors.New("укажите адрес сайта"))
			return
		}
		reply(w, a.ExplainRoute(r.Context(), host, r.URL.Query().Get("source")), nil)
	})
	m.HandleFunc("GET /api/auto", func(w http.ResponseWriter, r *http.Request) {
		groups, lat := a.AutoStatus()
		reply(w, map[string]any{"groups": groups, "latency": lat}, nil)
	})
	m.HandleFunc("POST /api/devices/cleanup", func(w http.ResponseWriter, r *http.Request) {
		n, err := a.CleanupDevices()
		reply(w, map[string]int{"removed": n}, err)
	})
	m.HandleFunc("PUT /api/devices/bulk", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs         []string `json:"ids"`
			Route       *string  `json:"route"`
			RouteTarget *string  `json:"routeTarget"`
			Pinned      *bool    `json:"pinned"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := a.Store.Update(func(c *model.Config) error {
			for i := range c.Devices {
				d := &c.Devices[i]
				if !slices.Contains(in.IDs, d.ID) {
					continue
				}
				if in.Route != nil {
					d.Route = *in.Route
				}
				if in.RouteTarget != nil {
					d.RouteTarget = *in.RouteTarget
				}
				if in.Pinned != nil && d.IP != "" {
					d.Pinned = *in.Pinned
				}
			}
			return nil
		})
		if err == nil {
			a.DevicesChanged()
		}
		reply(w, nil, err)
	})

	m.HandleFunc("POST /api/nodes/import", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text string `json:"text"`
		}
		if !decode(w, r, &in) {
			return
		}
		added, errs, err := a.ImportLinks(in.Text, "")
		reply(w, map[string]any{"added": added, "errors": errs}, err)
	})
	m.HandleFunc("POST /api/nodes/test", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs []string `json:"ids"`
		}
		if !decode(w, r, &in) {
			return
		}
		reply(w, a.TestNodes(r.Context(), in.IDs), nil)
	})
	m.HandleFunc("POST /api/subscriptions/{id}/update", func(w http.ResponseWriter, r *http.Request) {
		n, changed, err := a.UpdateSubscription(r.Context(), r.PathValue("id"))
		reply(w, map[string]any{"nodes": n, "changed": changed}, err)
	})
	// Удаление подписки удаляет и её узлы.
	m.HandleFunc("POST /api/subscriptions/{id}/purge", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		err := a.Store.Update(func(c *model.Config) error {
			c.Nodes = slices.DeleteFunc(c.Nodes, func(n model.Node) bool { return n.SubscriptionID == id })
			c.Subscriptions = slices.DeleteFunc(c.Subscriptions, func(s model.Subscription) bool { return s.ID == id })
			return nil
		})
		if err == nil {
			a.Changed()
		}
		reply(w, nil, err)
	})
	m.HandleFunc("PUT /api/rules/order", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs []string `json:"ids"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := a.Store.Update(func(c *model.Config) error {
			if len(in.IDs) != len(c.Rules) {
				return errors.New("список правил устарел, обновите страницу")
			}
			sorted := make([]model.Rule, 0, len(c.Rules))
			for _, id := range in.IDs {
				i := slices.IndexFunc(c.Rules, func(r model.Rule) bool { return r.ID == id })
				if i < 0 {
					return errors.New("список правил устарел, обновите страницу")
				}
				sorted = append(sorted, c.Rules[i])
			}
			c.Rules = sorted
			return nil
		})
		if err == nil {
			a.Changed()
		}
		reply(w, nil, err)
	})

	// Веб-интерфейс: ETag, gzip, вечный кэш для файлов с хэшем в имени (static.go).
	m.Handle("/", newStaticServer(static))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Защита от CSRF: интерфейс доступен в локальной сети, поэтому изменяющие
	// запросы принимаем только со своей страницы.
	if r.Method != http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/") {
		if origin := r.Header.Get("Origin"); origin != "" && !sameHost(origin, r.Host) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	if !s.requireSession(w, r) {
		return
	}
	s.mux.ServeHTTP(w, r)
}

// collection регистрирует CRUD для списка объектов с полем ID.
// check (может быть nil) проверяет объект перед сохранением.
func collection[T any](s *Server, name string, list func(*model.Config) *[]T, idOf func(*T) *string, check func(*T) error, changed func()) {
	a := s.app
	base := "/api/" + name
	find := func(items []T, id string) int {
		return slices.IndexFunc(items, func(it T) bool { return *idOf(&it) == id })
	}

	s.mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		cfg := a.Store.Get()
		items := *list(cfg)
		if items == nil {
			items = []T{}
		}
		reply(w, items, nil)
	})
	s.mux.HandleFunc("POST "+base, func(w http.ResponseWriter, r *http.Request) {
		var item T
		if !decode(w, r, &item) {
			return
		}
		if check != nil {
			if err := check(&item); err != nil {
				reply(w, nil, err)
				return
			}
		}
		*idOf(&item) = store.NewID()
		err := a.Store.Update(func(c *model.Config) error {
			*list(c) = append(*list(c), item)
			return nil
		})
		if err == nil {
			changed()
		}
		reply(w, item, err)
	})
	s.mux.HandleFunc("PUT "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		var item T
		if !decode(w, r, &item) {
			return
		}
		if check != nil {
			if err := check(&item); err != nil {
				reply(w, nil, err)
				return
			}
		}
		id := r.PathValue("id")
		*idOf(&item) = id
		err := a.Store.Update(func(c *model.Config) error {
			i := find(*list(c), id)
			if i < 0 {
				return errNotFound
			}
			(*list(c))[i] = item
			return nil
		})
		if err == nil {
			changed()
		}
		reply(w, item, err)
	})
	s.mux.HandleFunc("DELETE "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		err := a.Store.Update(func(c *model.Config) error {
			i := find(*list(c), id)
			if i < 0 {
				return errNotFound
			}
			*list(c) = slices.Delete(*list(c), i, i+1)
			return nil
		})
		if err == nil {
			changed()
		}
		reply(w, nil, err)
	})
}

var errNotFound = errors.New("не найдено")

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		reply(w, nil, errors.New("неверный запрос: "+err.Error()))
		return false
	}
	return true
}

func replyStatus(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func reply(w http.ResponseWriter, data any, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errNotFound) {
			status = http.StatusNotFound
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(data)
}

func sameHost(origin, host string) bool {
	o := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")
	return o == host
}

func orNull(b []byte) []byte {
	if len(b) == 0 {
		return []byte("null")
	}
	return b
}
