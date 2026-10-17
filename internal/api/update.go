package api

import "net/http"

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
		}
		if !decode(w, r, &req) {
			return
		}
		reply(w, u.State(), u.SetPrefs(req.Repo, req.Token, req.AutoCheck))
	})
	m.HandleFunc("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) {
		st, err := u.Check(r.Context())
		reply(w, st, err)
	})
	m.HandleFunc("POST /api/update/install", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"restarting": true}, u.Install(r.Context()))
	})
	m.HandleFunc("POST /api/update/rollback", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"restarting": true}, u.Rollback())
	})
}
