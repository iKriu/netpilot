package api

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"netpilot/internal/auth"
)

// registerAuth — вход, первый запуск, смена пароля, выход.
// Эти адреса доступны без сессии (кроме смены пароля).
func (s *Server) registerAuth() {
	m, au := s.mux, s.auth

	m.HandleFunc("GET /api/auth/state", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"setup": !au.Configured(), "authenticated": au.Valid(sessionToken(r))}, nil)
	})
	m.HandleFunc("POST /api/auth/setup", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Password string `json:"password"`
		}
		if !decode(w, r, &in) {
			return
		}
		tok, err := au.Setup(in.Password)
		if err == nil {
			setSession(w, r, tok)
		}
		reply(w, nil, err)
	})
	m.HandleFunc("POST /api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Password string `json:"password"`
		}
		if !decode(w, r, &in) {
			return
		}
		tok, err := au.Login(clientIP(r), in.Password)
		if err != nil {
			// Небольшая пауза делает подбор ещё медленнее.
			time.Sleep(400 * time.Millisecond)
			var locked auth.LockedError
			if errors.As(err, &locked) {
				replyStatus(w, http.StatusTooManyRequests, err)
				return
			}
			replyStatus(w, http.StatusUnauthorized, err)
			return
		}
		setSession(w, r, tok)
		reply(w, nil, nil)
	})
	m.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		au.Logout(sessionToken(r))
		http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		reply(w, nil, nil)
	})
	m.HandleFunc("POST /api/auth/password", func(w http.ResponseWriter, r *http.Request) {
		if !au.Valid(sessionToken(r)) {
			replyStatus(w, http.StatusUnauthorized, errors.New("требуется вход"))
			return
		}
		var in struct {
			Current string `json:"current"`
			Next    string `json:"next"`
		}
		if !decode(w, r, &in) {
			return
		}
		reply(w, nil, au.ChangePassword(clientIP(r), in.Current, in.Next, sessionToken(r)))
	})
}

// requireSession пропускает к API только с действующей сессией.
func (s *Server) requireSession(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/api/auth/") {
		return true
	}
	if s.auth.Valid(sessionToken(r)) {
		return true
	}
	replyStatus(w, http.StatusUnauthorized, errors.New("требуется вход"))
	return false
}

func sessionToken(r *http.Request) string {
	c, err := r.Cookie(auth.CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func setSession(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: auth.CookieName, Value: token, Path: "/",
		MaxAge:   int(auth.SessionTTL.Seconds()),
		HttpOnly: true,                    // недоступна скриптам страницы
		SameSite: http.SameSiteStrictMode, // не отправляется с чужих сайтов
		Secure:   r.TLS != nil,
	})
}

// clientIP — адрес клиента. Заголовкам X-Forwarded-For не доверяем:
// их легко подделать и обойти блокировку подбора пароля.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
