package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/PacificDailyTimes/pdt-news/internal/flags"
	"github.com/PacificDailyTimes/pdt-news/internal/pay"
)

func (s *Server) setting(k string) string {
	pool, err := s.db()
	if err != nil {
		return ""
	}
	var v string
	_ = pool.QueryRow(context.Background(), `SELECT v FROM settings WHERE k=$1 ORDER BY site_id LIMIT 1`, k).Scan(&v)
	return v
}

func (s *Server) setSetting(k, v string) {
	pool, err := s.db()
	if err != nil {
		return
	}
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO settings(site_id,k,v) VALUES(1,$1,$2) ON CONFLICT (site_id,k) DO UPDATE SET v=EXCLUDED.v`, k, v)
}

func (s *Server) flags() flags.Set {
	return flags.Set{
		Shop:         flags.Merge(s.cfg.EnableShop, s.setting("shop")),
		Subs:         flags.Merge(s.cfg.EnableSubs, s.setting("subs")),
		Appointments: flags.Merge(s.cfg.EnableAppt, s.setting("appointments")),
	}
}

func (s *Server) payCfg() pay.Cfg {
	c := pay.Cfg{
		StripeSecret:  nz(s.cfg.StripeSecret, s.setting("stripe_secret")),
		StripePub:     nz(s.cfg.StripePub, s.setting("stripe_publishable")),
		PaypalID:      nz(s.cfg.PaypalID, s.setting("paypal_client_id")),
		PaypalSecret:  nz(s.cfg.PaypalSecret, s.setting("paypal_secret")),
		PaypalSandbox: s.cfg.PaypalSandbox || s.setting("paypal_sandbox") == "1",
		Origin:        s.cfg.URL,
	}
	for t, coin := range s.cfg.Coins {
		if coin.Address != "" {
			c.Coins = append(c.Coins, pay.Coin{Ticker: strings.ToUpper(t), Address: coin.Address})
		}
	}
	return c
}

func (s *Server) payLocked() bool {
	return s.cfg.StripeSecret != "" || s.cfg.PaypalID != ""
}

func (s *Server) decorate(p *page, place string) {
	p.Flags = s.flags()
	p.Place = place
	p.Corners = "square"
	p.BookWord = "appointment"
	pool, err := s.db()
	if err != nil {
		return
	}
	var corners, book, seot, seod, seoi, robots, social string
	_ = pool.QueryRow(context.Background(),
		`SELECT coalesce(corners,'square'), coalesce(book_word,'appointment'),
		        coalesce(seo_title,''), coalesce(seo_desc,''), coalesce(seo_image,''), coalesce(robots,'index,follow'),
		        coalesce(social::text,'{}')
		 FROM sites WHERE is_main=true LIMIT 1`).
		Scan(&corners, &book, &seot, &seod, &seoi, &robots, &social)
	if corners != "" {
		p.Corners = corners
	}
	if book != "" {
		p.BookWord = book
	}
	if p.SEOTitle == "" {
		p.SEOTitle = seot
	}
	if p.SEODesc == "" {
		p.SEODesc = seod
	}
	if p.SEOImage == "" {
		p.SEOImage = seoi
	}
	if p.Robots == "" {
		p.Robots = robots
	}
	var sm map[string]string
	_ = json.Unmarshal([]byte(social), &sm)
	for k, v := range sm {
		if v != "" {
			p.Social = append(p.Social, map[string]string{"Net": k, "URL": v})
		}
	}
	p.HeaderNav = s.navItems("header")
	p.FooterNav = s.navItems("footer")
	if place != "" {
		p.AboveNav = s.navItems("above-" + place)
		p.BelowNav = s.navItems("below-" + place)
	}
}

func (s *Server) navItems(place string) []map[string]any {
	pool, err := s.db()
	if err != nil {
		return nil
	}
	rows, err := pool.Query(context.Background(),
		`SELECT i.label, i.href, i.kind FROM nav_items i JOIN navs n ON n.id=i.nav_id
		 WHERE n.places LIKE '%'||$1||'%' ORDER BY i.pos, i.id`, place)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var l, h, k string
		_ = rows.Scan(&l, &h, &k)
		out = append(out, map[string]any{"Label": l, "Href": h, "Kind": k})
	}
	if len(out) == 0 && place == "header" {
		r2, _ := pool.Query(context.Background(), `SELECT label, href FROM menus ORDER BY pos`)
		if r2 != nil {
			defer r2.Close()
			for r2.Next() {
				var l, h string
				_ = r2.Scan(&l, &h)
				out = append(out, map[string]any{"Label": l, "Href": h, "Kind": "link"})
			}
		}
	}
	return out
}

func (s *Server) userRank(uid int64) int {
	if uid == 0 {
		return 0
	}
	pool, err := s.db()
	if err != nil {
		return 0
	}
	var rank int
	_ = pool.QueryRow(context.Background(),
		`SELECT coalesce(max(t.rank),0) FROM entitlements e
		 JOIN tiers t ON t.product_id=e.product_id WHERE e.user_id=$1`, uid).Scan(&rank)
	var until *string
	_ = pool.QueryRow(context.Background(),
		`SELECT member_until::text FROM users WHERE id=$1 AND member_until > now()`, uid).Scan(&until)
	if until != nil && rank < 1 {
		rank = 1
	}
	return rank
}

func (s *Server) gate(w http.ResponseWriter, r *http.Request, min int, title string) bool {
	if min <= 0 {
		return true
	}
	u := s.user(r)
	uid := int64(0)
	if u != nil {
		uid = u.ID
		if u.Role == "admin" {
			return true
		}
	}
	if s.userRank(uid) >= min {
		return true
	}
	p := s.base(r, title)
	s.decorate(&p, "page")
	p.Flash = "This is for members at rank " + strconv.Itoa(min) + " and up."
	s.render(w, "paywall.html", p)
	return false
}

func (s *Server) dashSite(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "admin")
	if u == nil {
		return
	}
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		act := r.FormValue("act")
		switch act {
		case "look":
			social, _ := json.Marshal(map[string]string{
				"x": r.FormValue("social_x"), "github": r.FormValue("social_github"),
				"youtube": r.FormValue("social_youtube"), "facebook": r.FormValue("social_facebook"),
				"instagram": r.FormValue("social_instagram"), "rss": r.FormValue("social_rss"),
			})
			_, _ = pool.Exec(context.Background(),
				`UPDATE sites SET corners=$1, book_word=$2, social=$3::jsonb, seo_title=$4, seo_desc=$5, seo_image=$6, robots=$7, tagline=$8, description=$9
				 WHERE is_main=true`,
				val(r, "corners", "square"), val(r, "book_word", "appointment"), string(social),
				r.FormValue("seo_title"), r.FormValue("seo_desc"), r.FormValue("seo_image"),
				val(r, "robots", "index,follow"), r.FormValue("tagline"), r.FormValue("description"))
		case "flags":
			fl := s.flags()
			if !fl.Shop.Locked {
				s.setSetting("shop", onOff(r.FormValue("shop")))
			}
			if !fl.Subs.Locked {
				s.setSetting("subs", onOff(r.FormValue("subs")))
			}
			if !fl.Appointments.Locked {
				s.setSetting("appointments", onOff(r.FormValue("appointments")))
			}
		case "pay":
			if !s.payLocked() {
				for _, k := range []string{"stripe_secret", "stripe_publishable", "paypal_client_id", "paypal_secret", "paypal_sandbox"} {
					s.setSetting(k, r.FormValue(k))
				}
			}
		case "nav":
			name := val(r, "nav_name", "Main")
			places := strings.Join(r.Form["place"], ",")
			var nid int64
			_ = pool.QueryRow(context.Background(),
				`INSERT INTO navs(site_id,name,places) SELECT id,$1,$2 FROM sites WHERE is_main=true RETURNING id`,
				name, places).Scan(&nid)
		case "navitem":
			nid, _ := strconv.ParseInt(r.FormValue("nav_id"), 10, 64)
			_, _ = pool.Exec(context.Background(),
				`INSERT INTO nav_items(nav_id,label,href,kind,pos) VALUES($1,$2,$3,$4,coalesce((SELECT max(pos)+1 FROM nav_items WHERE nav_id=$1),0))`,
				nid, r.FormValue("label"), r.FormValue("href"), val(r, "kind", "link"))
		case "navmove":
			id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
			dir := r.FormValue("dir")
			if dir == "up" {
				_, _ = pool.Exec(context.Background(), `UPDATE nav_items SET pos=pos-1 WHERE id=$1`, id)
			} else {
				_, _ = pool.Exec(context.Background(), `UPDATE nav_items SET pos=pos+1 WHERE id=$1`, id)
			}
		case "navdel":
			id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
			_, _ = pool.Exec(context.Background(), `DELETE FROM nav_items WHERE id=$1`, id)
		}
		http.Redirect(w, r, "/dash/site", http.StatusSeeOther)
		return
	}
	p := s.base(r, "Site")
	s.decorate(&p, "")
	var corners, book, seot, seod, seoi, robots, tagline, descr, social string
	_ = pool.QueryRow(context.Background(),
		`SELECT coalesce(corners,'square'),coalesce(book_word,'appointment'),coalesce(seo_title,''),coalesce(seo_desc,''),
		        coalesce(seo_image,''),coalesce(robots,'index,follow'),coalesce(tagline,''),coalesce(description,''),coalesce(social::text,'{}')
		 FROM sites WHERE is_main=true`).Scan(&corners, &book, &seot, &seod, &seoi, &robots, &tagline, &descr, &social)
	var sm map[string]string
	_ = json.Unmarshal([]byte(social), &sm)
	navs := []map[string]any{}
	nr, _ := pool.Query(context.Background(), `SELECT id,name,places FROM navs ORDER BY id`)
	for nr.Next() {
		var id int64
		var n, pl string
		_ = nr.Scan(&id, &n, &pl)
		items := []map[string]any{}
		ir, _ := pool.Query(context.Background(), `SELECT id,label,href,kind,pos FROM nav_items WHERE nav_id=$1 ORDER BY pos,id`, id)
		for ir.Next() {
			var iid int64
			var l, h, k string
			var pos int
			_ = ir.Scan(&iid, &l, &h, &k, &pos)
			items = append(items, map[string]any{"ID": iid, "Label": l, "Href": h, "Kind": k})
		}
		ir.Close()
		navs = append(navs, map[string]any{"ID": id, "Name": n, "Places": pl, "Items": items})
	}
	nr.Close()
	p.Data = map[string]any{
		"Corners": corners, "BookWord": book, "SEOTitle": seot, "SEODesc": seod, "SEOImage": seoi,
		"Robots": robots, "Tagline": tagline, "Descr": descr, "Social": sm, "Navs": navs,
		"PayLocked": s.payLocked(),
		"Stripe":    nz(s.cfg.StripeSecret, s.setting("stripe_secret")) != "",
		"Paypal":    nz(s.cfg.PaypalID, s.setting("paypal_client_id")) != "",
		"Flags":     s.flags(),
	}
	s.render(w, "dash-site.html", p)
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "User-agent: *\nAllow: /\nSitemap: %s/sitemap.xml\n", strings.TrimRight(s.cfg.URL, "/"))
}

func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	pool, err := s.db()
	if err != nil {
		http.Error(w, "db", 500)
		return
	}
	origin := strings.TrimRight(s.cfg.URL, "/")
	w.Header().Set("Content-Type", "application/xml")
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	fmt.Fprintf(w, "<url><loc>%s/</loc></url>", origin)
	rows, _ := pool.Query(context.Background(), `SELECT slug FROM pieces WHERE status='live'`)
	for rows.Next() {
		var sl string
		_ = rows.Scan(&sl)
		fmt.Fprintf(w, "<url><loc>%s/%s</loc></url>", origin, sl)
	}
	rows.Close()
	pr, _ := pool.Query(context.Background(), `SELECT slug FROM products WHERE active=true`)
	for pr.Next() {
		var sl string
		_ = pr.Scan(&sl)
		fmt.Fprintf(w, "<url><loc>%s/shop/%s</loc></url>", origin, sl)
	}
	pr.Close()
	fmt.Fprint(w, `</urlset>`)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	p := s.base(r, "Search")
	s.decorate(&p, "page")
	var list []map[string]any
	if q != "" {
		pool, _ := s.db()
		rows, _ := pool.Query(context.Background(),
			`SELECT title,slug,excerpt FROM pieces WHERE status='live' AND (title ILIKE $1 OR content ILIKE $1) LIMIT 40`, "%"+q+"%")
		for rows.Next() {
			var t, sl, ex string
			_ = rows.Scan(&t, &sl, &ex)
			list = append(list, map[string]any{"Title": t, "Slug": sl, "Excerpt": ex})
		}
		rows.Close()
	}
	p.Data = map[string]any{"Q": q, "Hits": list}
	s.render(w, "search.html", p)
}

func onOff(v string) string {
	if v == "1" || strings.EqualFold(v, "on") {
		return "1"
	}
	return "0"
}

func nz(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
