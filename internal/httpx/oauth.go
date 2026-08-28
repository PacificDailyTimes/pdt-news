package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/PacificDailyTimes/pdt-news/internal/db"
	"github.com/PacificDailyTimes/pdt-news/internal/oauth"
)

func (s *Server) authStart(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/auth/")
	p = strings.TrimSuffix(p, "/callback")
	if strings.Contains(p, "/") {
		p = strings.Split(p, "/")[0]
	}
	if r.URL.Query().Get("code") != "" || strings.HasSuffix(r.URL.Path, "/callback") {
		s.authCallback(w, r, p)
		return
	}
	st := tok(16)
	http.SetCookie(w, &http.Cookie{Name: "pdt_oauth", Value: st, Path: "/", HttpOnly: true, MaxAge: 600, SameSite: http.SameSiteLaxMode})
	if r.URL.Query().Get("link") == "1" {
		http.SetCookie(w, &http.Cookie{Name: "pdt_oauth_link", Value: "1", Path: "/", HttpOnly: true, MaxAge: 600})
	}
	cb := strings.TrimRight(s.cfg.URL, "/") + "/auth/" + p + "/callback"
	var u string
	var err error
	if r.URL.Query().Get("cal") == "1" {
		http.SetCookie(w, &http.Cookie{Name: "pdt_oauth_cal", Value: "1", Path: "/", HttpOnly: true, MaxAge: 600})
		u, err = oauth.GoogleCalStart(s.cfg, st, cb)
	} else {
		u, err = oauth.StartURL(s.cfg, p, st, cb)
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	http.Redirect(w, r, u, http.StatusFound)
}

func (s *Server) authCallback(w http.ResponseWriter, r *http.Request, provider string) {
	c, _ := r.Cookie("pdt_oauth")
	if c == nil || c.Value != r.URL.Query().Get("state") {
		http.Error(w, "state", 400)
		return
	}
	cb := strings.TrimRight(s.cfg.URL, "/") + "/auth/" + provider + "/callback"
	if lc, err := r.Cookie("pdt_oauth_cal"); err == nil && lc.Value == "1" {
		http.SetCookie(w, &http.Cookie{Name: "pdt_oauth_cal", Value: "", Path: "/", MaxAge: -1})
		s.googleCalCallback(w, r, cb)
		return
	}
	prof, err := oauth.Finish(s.cfg, provider, r.URL.Query().Get("code"), cb)
	if err != nil || prof == nil || prof.Subject == "" {
		http.Error(w, "oauth failed", 400)
		return
	}
	pool, err := s.db()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	link := false
	if lc, err := r.Cookie("pdt_oauth_link"); err == nil && lc.Value == "1" {
		link = true
	}
	if link {
		u := s.user(r)
		if u == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO oauth_identities(user_id,provider,subject,email) VALUES($1,$2,$3,$4)
			 ON CONFLICT (provider,subject) DO NOTHING`, u.ID, provider, prof.Subject, prof.Email)
		http.Redirect(w, r, "/dash/security", http.StatusSeeOther)
		return
	}
	var uid int64
	err = pool.QueryRow(context.Background(),
		`SELECT user_id FROM oauth_identities WHERE provider=$1 AND subject=$2`, provider, prof.Subject).Scan(&uid)
	if err != nil && prof.Email != "" {
		u, e2 := db.UserByEmail(pool, prof.Email)
		if e2 == nil {
			uid = u.ID
			_, _ = pool.Exec(context.Background(),
				`INSERT INTO oauth_identities(user_id,provider,subject,email) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
				uid, provider, prof.Subject, prof.Email)
		}
	}
	if uid == 0 {
		login := slugify(strings.Split(prof.Email, "@")[0])
		if len(login) < 4 {
			login = "user" + tok(3)
		}
		base := login
		for i := 0; i < 20; i++ {
			uid, err = db.CreateUser(pool, login, firstNonEmpty(prof.Email, login+"@oauth.local"), "consumer", prof.Name, nil, nil)
			if err == nil {
				break
			}
			login = base + tok(2)
			uid = 0
		}
		if uid == 0 {
			http.Error(w, "could not create account", 500)
			return
		}
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO oauth_identities(user_id,provider,subject,email) VALUES($1,$2,$3,$4)`,
			uid, provider, prof.Subject, prof.Email)
	}
	u, err := db.UserByID(pool, uid)
	if err != nil {
		http.Error(w, "user", 500)
		return
	}
	if u.TOTPOn {
		t := tok(12)
		http.SetCookie(w, &http.Cookie{Name: "pdt_2fa", Value: fmt.Sprintf("%d:%s", uid, t), Path: "/", HttpOnly: true, MaxAge: 300})
		http.Redirect(w, r, "/login?totp=1", http.StatusSeeOther)
		return
	}
	s.setSession(w, uid)
	http.Redirect(w, r, "/dash", http.StatusSeeOther)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Server) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	u := s.user(r)
	if u == nil {
		http.Redirect(w, r, "/login?next=/oauth/authorize?"+r.URL.RawQuery, http.StatusSeeOther)
		return
	}
	cid := r.URL.Query().Get("client_id")
	redir := r.URL.Query().Get("redirect_uri")
	if cid == "" || cid != s.cfg.OAuthClientID || redir == "" {
		http.Error(w, "client", 400)
		return
	}
	if r.Method == http.MethodGet {
		s.render(w, "oauth-consent.html", map[string]any{"Redirect": redir, "Client": cid, "State": r.URL.Query().Get("state")})
		return
	}
	_ = r.ParseForm()
	code := tok(24)
	pool, _ := s.db()
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO oauth_codes(code,user_id,client_id,redirect,expires) VALUES($1,$2,$3,$4,now()+interval '10 minutes')`,
		code, u.ID, cid, redir)
	sep := "?"
	if strings.Contains(redir, "?") {
		sep = "&"
	}
	http.Redirect(w, r, redir+sep+"code="+code+"&state="+r.FormValue("state"), http.StatusSeeOther)
}

func (s *Server) oauthToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.FormValue("client_id") != s.cfg.OAuthClientID || r.FormValue("client_secret") != s.cfg.OAuthClientSecret {
		http.Error(w, "client", 401)
		return
	}
	pool, _ := s.db()
	var uid int64
	err := pool.QueryRow(context.Background(),
		`DELETE FROM oauth_codes WHERE code=$1 AND expires>now() RETURNING user_id`, r.FormValue("code")).Scan(&uid)
	if err != nil {
		http.Error(w, "code", 400)
		return
	}
	tokn := tok(24)
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO oauth_tokens(token,user_id,client_id,expires) VALUES($1,$2,$3,now()+interval '30 days')`,
		tokn, uid, s.cfg.OAuthClientID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": tokn, "token_type": "bearer", "expires_in": 2592000})
}

func (s *Server) oauthUserinfo(w http.ResponseWriter, r *http.Request) {
	h := r.Header.Get("Authorization")
	tokn := strings.TrimPrefix(h, "Bearer ")
	pool, _ := s.db()
	var uid int64
	err := pool.QueryRow(context.Background(),
		`SELECT user_id FROM oauth_tokens WHERE token=$1 AND expires>now()`, tokn).Scan(&uid)
	if err != nil {
		http.Error(w, "token", 401)
		return
	}
	u, err := db.UserByID(pool, uid)
	if err != nil {
		http.Error(w, "user", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": u.ID, "login_id": u.LoginID, "email": u.Email, "name": u.Name, "handle": u.PublicHandle(),
	})
}
