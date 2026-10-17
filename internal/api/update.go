package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"netpilot/internal/auth"
	"netpilot/internal/update"
)

// Обновление программы: проверка, установка по кнопке и откат.
func (s *Server) registerUpdate() {
	m, u := s.mux, s.upd
	m.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) {
		reply(w, u.State(), nil)
	})
	m.HandleFunc("PUT /api/update", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Repo      string  `json:"repo"`
			Token     *string `json:"token"`
			AutoCheck bool    `json:"autoCheck"`
			Password  string  `json:"password"`
		}
		if !decode(w, r, &req) {
			return
		}
		// Источник обновлений решает, какой код запустится с правами
		// программы, поэтому его смену подтверждаем паролем: одной украденной
		// сессии (открытая вкладка, чужой компьютер) для этого мало.
		if u.ChangesSource(req.Repo, req.Token) {
			if req.Password == "" {
				replyStatus(w, http.StatusForbidden, errors.New("для смены источника обновлений введите текущий пароль"))
				return
			}
			if err := s.auth.Verify(clientIP(r), req.Password); err != nil {
				time.Sleep(400 * time.Millisecond) // как при входе: подбор медленнее
				var locked auth.LockedError
				if errors.As(err, &locked) || errors.Is(err, auth.ErrBusy) {
					replyStatus(w, http.StatusTooManyRequests, err)
					return
				}
				// 403, а не 401: интерфейс по 401 считает сессию завершённой
				// и выкидывает на экран входа, а сессия здесь в порядке.
				replyStatus(w, http.StatusForbidden, err)
				return
			}
		}
		reply(w, u.State(), u.SetPrefs(req.Repo, req.Token, req.AutoCheck))
	})
	m.HandleFunc("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) {
		st, err := u.Check(r.Context())
		reply(w, st, err)
	})
	m.HandleFunc("POST /api/update/install", func(w http.ResponseWriter, r *http.Request) {
		// Установка не должна обрываться, если вкладку закрыли или связь
		// мигнула: недокачанный файл — потерянное время, а ход установки
		// интерфейс всё равно узнаёт через GET /api/update.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), update.InstallTimeout)
		defer cancel()
		reply(w, map[string]bool{"restarting": true}, u.Install(ctx))
	})
	m.HandleFunc("POST /api/update/rollback", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"restarting": true}, u.Rollback())
	})
}
