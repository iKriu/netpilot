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
		setup := !au.Configured()
		reply(w, map[string]bool{
			"setup":         setup,
			"setupCode":     setup && !isLoopback(r), // интерфейс покажет поле для кода из журнала
			"authenticated": au.Valid(sessionToken(r)),
		}, nil)
	})
	m.HandleFunc("POST /api/auth/setup", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Password string `json:"password"`
			Code     string `json:"code"`
		}
		if !decode(w, r, &in) {
			return
		}
		// С этого же компьютера код не нужен; из сети — только с кодом из журнала,
		// иначе пароль успел бы задать любой, кто первым откроет страницу.
		var tok string
		var err error
		if isLoopback(r) {
			tok, err = au.Setup(in.Password)
		} else {
			tok, err = au.SetupWithCode(clientIP(r), in.Code, in.Password)
		}
		if err != nil {
			var locked auth.LockedError
			switch {
			case errors.As(err, &locked):
				replyStatus(w, http.StatusTooManyRequests, err)
			case errors.Is(err, auth.ErrSetupCode):
				time.Sleep(400 * time.Millisecond) // замедляем перебор кода
				replyStatus(w, http.StatusForbidden, err)
			default:
				reply(w, nil, err)
			}
			return
		}
		setSession(w, r, tok)
		reply(w, nil, nil)
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
			if errors.As(err, &locked) || errors.Is(err, auth.ErrBusy) {
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

// isLoopback — запрос с этого же компьютера. Смотрим только на адрес
// соединения: X-Forwarded-For подделывается.
func isLoopback(r *http.Request) bool {
	ip := net.ParseIP(clientIP(r))
	return ip != nil && ip.IsLoopback()
}
