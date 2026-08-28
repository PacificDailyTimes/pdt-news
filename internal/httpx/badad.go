package httpx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (s *Server) dashBadAd(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "admin", "author")
	if u == nil {
		return
	}
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		if r.FormValue("link") == "1" && s.cfg.BadAdURL != "" {
			st := tok(12)
			http.SetCookie(w, &http.Cookie{Name: "pdt_badad", Value: st, Path: "/", HttpOnly: true, MaxAge: 600})
			cb := strings.TrimRight(s.cfg.URL, "/") + "/dash/badad"
			q := url.Values{"client_id": {s.cfg.OAuthClientID}, "redirect_uri": {cb}, "response_type": {"code"}, "state": {st}}
			// Login-with-pdt is initiated FROM badAd; here we send the user to badAd's link-pdt endpoint
			http.Redirect(w, r, s.cfg.BadAdURL+"/login/pdt?from=pdt&"+q.Encode(), http.StatusSeeOther)
			return
		}
		if r.FormValue("mint") == "1" {
			pub, sec := "pub_"+tok(24), "sec_"+tok(24)
			_, _ = pool.Exec(context.Background(),
				`INSERT INTO badad_links(user_id,pub_key,sec_key) VALUES($1,$2,$3)
				 ON CONFLICT (user_id) DO UPDATE SET pub_key=EXCLUDED.pub_key, sec_key=EXCLUDED.sec_key, linked_at=now()`,
				u.ID, pub, sec)
			if s.cfg.BadAdURL != "" {
				go postJSON(s.cfg.BadAdURL+"/api/pdt/keys", map[string]string{
					"pdt_user": strconv.FormatInt(u.ID, 10), "pub": pub, "sec": sec, "secret": s.cfg.OAuthClientSecret,
				})
			}
		}
		http.Redirect(w, r, "/dash/badad", http.StatusSeeOther)
		return
	}
	var pub, sec, buser string
	_ = pool.QueryRow(context.Background(),
		`SELECT coalesce(pub_key,''), coalesce(sec_key,''), coalesce(badad_user,'') FROM badad_links WHERE user_id=$1`, u.ID).
		Scan(&pub, &sec, &buser)
	if pub == "" && s.cfg.BadAdPub != "" {
		pub, sec = s.cfg.BadAdPub, s.cfg.BadAdSec
	}
	p := s.base(r, "badAd")
	p.Data = map[string]any{"Pub": pub, "Sec": sec, "Linked": buser != "" || pub != "", "URL": s.cfg.BadAdURL, "Auto": buser != ""}
	s.render(w, "badad.html", p)
}

func postJSON(u string, m map[string]string) {
	b, _ := json.Marshal(m)
	resp, err := http.Post(u, "application/json", strings.NewReader(string(b)))
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}
