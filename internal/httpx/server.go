package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PacificDailyTimes/pdt-news/internal/agg"
	"github.com/PacificDailyTimes/pdt-news/internal/config"
	"github.com/PacificDailyTimes/pdt-news/internal/db"
	"github.com/PacificDailyTimes/pdt-news/internal/mailer"
	"github.com/PacificDailyTimes/pdt-news/internal/tax"
	"github.com/PacificDailyTimes/pdt-news/internal/totp"
	"github.com/PacificDailyTimes/pdt-news/internal/wallet"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Server struct {
	cfg       *config.Config
	root      string
	pool      *pgxpool.Pool
	tmpl      *template.Template
	mux       *http.ServeMux
	mediaOnce sync.Once
}

func Listen(cfg *config.Config, root string) error {
	s := &Server{cfg: cfg, root: root, mux: http.NewServeMux()}
	s.parseTmpl()
	s.routes()
	if p, err := s.db(); err == nil {
		go agg.Loop(p)
	}
	return http.ListenAndServe(cfg.Addr(), s.mux)
}

func (s *Server) parseTmpl() {
	funcMap := template.FuncMap{
		"safe": func(s string) template.HTML { return template.HTML(s) },
		"h":    html.EscapeString,
	}
	t, err := template.New("x").Funcs(funcMap).ParseGlob(filepath.Join(s.root, "web/templates/*.html"))
	if err != nil {
		log.Printf("templates: %v", err)
		return
	}
	s.tmpl = t
}

func (s *Server) db() (*pgxpool.Pool, error) {
	if s.pool != nil {
		return s.pool, nil
	}
	if s.cfg.DBName == "" {
		return nil, fmt.Errorf("no database")
	}
	p, err := db.Connect(s.cfg.DSN())
	if err != nil {
		return nil, err
	}
	s.pool = p
	_ = db.Migrate(p, filepath.Join(s.root, "sql/schema.sql"))
	return p, nil
}

func (s *Server) routes() {
	s.mux.Handle("/static/", s.static())
	s.mux.Handle("/media/", http.StripPrefix("/media/", http.FileServer(http.Dir(filepath.Join(s.root, "var", "media")))))
	s.mux.HandleFunc("/install", s.install)
	s.mux.HandleFunc("/login", s.login)
	s.mux.HandleFunc("/auth/", s.authStart)
	s.mux.HandleFunc("/oauth/authorize", s.oauthAuthorize)
	s.mux.HandleFunc("/oauth/token", s.oauthToken)
	s.mux.HandleFunc("/oauth/userinfo", s.oauthUserinfo)
	s.mux.HandleFunc("/in/", s.magic)
	s.mux.HandleFunc("/logout", s.logout)
	s.mux.HandleFunc("/dash", s.dash)
	s.mux.HandleFunc("/dash/bio", s.dashBio)
	s.mux.HandleFunc("/dash/edit", s.edit)
	s.mux.HandleFunc("/dash/media", s.media)
	s.mux.HandleFunc("/dash/series", s.series)
	s.mux.HandleFunc("/dash/feeds", s.agg)
	s.mux.HandleFunc("/dash/shop", s.dashShop)
	s.mux.HandleFunc("/dash/wallet", s.dashWallet)
	s.mux.HandleFunc("/dash/security", s.security)
	s.mux.HandleFunc("/dash/badad", s.dashBadAd)
	s.mux.HandleFunc("/shop", s.shop)
	s.mux.HandleFunc("/checkout", s.checkout)
	s.mux.HandleFunc("/rss", s.feed("rss"))
	s.mux.HandleFunc("/atom", s.feed("atom"))
	s.mux.HandleFunc("/feed", s.feed("rss"))
	s.mux.HandleFunc("/", s.pub)
}

func (s *Server) static() http.Handler {
	return http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(s.root, "web/static"))))
}

func tok(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	if s.tmpl == nil {
		http.Error(w, "no templates", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Println(err)
	}
}

func (s *Server) user(r *http.Request) *db.User {
	c, err := r.Cookie("pdt")
	if err != nil || s.cfg.DBName == "" {
		return nil
	}
	pool, err := s.db()
	if err != nil {
		return nil
	}
	var uid int64
	err = pool.QueryRow(context.Background(),
		`SELECT user_id FROM sessions WHERE token=$1 AND expires > now()`, c.Value).Scan(&uid)
	if err != nil {
		return nil
	}
	u, _ := db.UserByID(pool, uid)
	return u
}

func (s *Server) setSession(w http.ResponseWriter, uid int64) {
	pool, _ := s.db()
	t := tok(24)
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO sessions(token,user_id,expires) VALUES($1,$2,now()+interval '14 days')`, t, uid)
	http.SetCookie(w, &http.Cookie{Name: "pdt", Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 14 * 86400})
}

type page struct {
	Title, Theme, Mode, URL, Flash, Err string
	Multi                               bool
	User                                *db.User
	Site                                map[string]any
	Data                                any
}

func (s *Server) base(r *http.Request, title string) page {
	p := page{Title: title, Theme: s.cfg.Theme, Mode: s.cfg.Mode, URL: s.cfg.URL, Multi: s.cfg.Multi(), User: s.user(r)}
	if p.Theme == "" {
		p.Theme = "masthead"
	}
	if pool, err := s.db(); err == nil {
		var name, tagline, theme string
		err := pool.QueryRow(context.Background(),
			`SELECT name, tagline, theme FROM sites WHERE is_main=true LIMIT 1`).Scan(&name, &tagline, &theme)
		if err != nil {
			name, theme = "pdt-news", p.Theme
		}
		p.Site = map[string]any{"name": name, "tagline": tagline, "theme": theme}
		if theme != "" {
			p.Theme = theme
		}
	}
	return p
}

func (s *Server) install(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.render(w, "install.html", map[string]any{"NeedPass": s.cfg.SetupPassword != "" || s.cfg.SetupHash != ""})
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	if !s.cfg.SetupOK(r.FormValue("setup_password")) {
		s.render(w, "install.html", map[string]any{"Err": "Setup password did not match.", "NeedPass": true})
		return
	}
	kv := map[string]string{
		"web_bind":       val(r, "web_bind", "127.0.0.1"),
		"web_port":       val(r, "web_port", "9001"),
		"web_url":        strings.TrimRight(r.FormValue("web_url"), "/"),
		"db_host":        val(r, "db_host", "127.0.0.1"),
		"db_port":        val(r, "db_port", "5432"),
		"db_name":        r.FormValue("db_name"),
		"db_user":        r.FormValue("db_user"),
		"db_pass":        r.FormValue("db_pass"),
		"mode":           val(r, "mode", "single"),
		"mail_transport": val(r, "mail_transport", "off"),
		"mail_from":      r.FormValue("mail_from"),
		"mail_from_name": val(r, "mail_from_name", "pdt-news"),
		"theme":          "masthead",
	}
	cfgPath := s.cfg.Path
	if cfgPath == "" {
		cfgPath = filepath.Join(s.root, "config")
	}
	if err := config.Write(cfgPath, kv); err != nil {
		s.render(w, "install.html", map[string]any{"Err": err.Error()})
		return
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		s.render(w, "install.html", map[string]any{"Err": err.Error()})
		return
	}
	s.cfg = cfg
	pool, err := db.Connect(cfg.DSN())
	if err != nil {
		s.render(w, "install.html", map[string]any{"Err": "postgres: " + err.Error()})
		return
	}
	s.pool = pool
	if err := db.Migrate(pool, filepath.Join(s.root, "sql/schema.sql")); err != nil {
		s.render(w, "install.html", map[string]any{"Err": "migrate: " + err.Error()})
		return
	}
	login := r.FormValue("login_id")
	email := r.FormValue("email")
	name := r.FormValue("name")
	if login == "" || email == "" {
		s.render(w, "install.html", map[string]any{"Err": "Login ID and email required."})
		return
	}
	var pass *string
	if p := r.FormValue("password"); p != "" {
		h, _ := bcrypt.GenerateFromPassword([]byte(p), 12)
		hs := string(h)
		pass = &hs
	}
	uid, err := db.CreateUser(pool, login, email, "admin", name, pass, nil)
	if err != nil {
		s.render(w, "install.html", map[string]any{"Err": err.Error()})
		return
	}
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO sites(slug,name,tagline,is_main) VALUES('main',$1,$2,true)`,
		val(r, "site_name", "pdt-news"), val(r, "tagline", ""))
	var sid int64
	_ = pool.QueryRow(context.Background(), `SELECT id FROM sites WHERE is_main=true`).Scan(&sid)
	_, _ = pool.Exec(context.Background(), `INSERT INTO site_members(site_id,user_id,can_post) VALUES($1,$2,true)`, sid, uid)
	_, _ = pool.Exec(context.Background(), `INSERT INTO series(site_id,name,slug) VALUES($1,'News','news')`, sid)
	_ = db.SetMeta(pool, "installed", "1")
	s.setSession(w, uid)
	http.Redirect(w, r, "/dash", http.StatusSeeOther)
}

func val(r *http.Request, k, def string) string {
	if v := r.FormValue(k); v != "" {
		return v
	}
	return def
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	p := s.base(r, "Sign in")
	if r.Method == http.MethodGet {
		s.render(w, "login.html", p)
		return
	}
	_ = r.ParseForm()
	pool, err := s.db()
	if err != nil {
		p.Err = err.Error()
		s.render(w, "login.html", p)
		return
	}
	login := r.FormValue("login_id")
	pass := r.FormValue("password")
	email := r.FormValue("email")
	if r.FormValue("magic") == "1" || pass == "" {
		u, err := db.UserByEmail(pool, email)
		if err != nil {
			u, err = db.UserByLogin(pool, login)
		}
		if err == nil {
			t := tok(24)
			_, _ = pool.Exec(context.Background(),
				`INSERT INTO magic_links(token,user_id,expires) VALUES($1,$2,now()+interval '1 hour')`, t, u.ID)
			link := s.cfg.URL + "/in/" + t
			_ = mailer.Send(s.cfg, u.Email, "Your "+p.Title+" sign-in",
				"Code: "+t[:8]+"\n\nOpen: "+link+"\n\nThis login ID never appears in public.\n")
		}
		p.Flash = "If that account exists, a sign-in link is on the way."
		s.render(w, "login.html", p)
		return
	}
	u, err := db.UserByLogin(pool, login)
	if err != nil || u.PassHash == nil || bcrypt.CompareHashAndPassword([]byte(*u.PassHash), []byte(pass)) != nil {
		p.Err = "Login ID or password did not match."
		s.render(w, "login.html", p)
		return
	}
	if u.TOTPOn {
		if !totp.Verify(*u.TOTP, r.FormValue("totp")) {
			p.Err = "Authenticator code required."
			s.render(w, "login.html", p)
			return
		}
	}
	s.setSession(w, u.ID)
	http.Redirect(w, r, "/dash", http.StatusSeeOther)
}

func (s *Server) magic(w http.ResponseWriter, r *http.Request) {
	t := strings.TrimPrefix(r.URL.Path, "/in/")
	pool, err := s.db()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var uid int64
	err = pool.QueryRow(context.Background(),
		`DELETE FROM magic_links WHERE token=$1 AND expires>now() RETURNING user_id`, t).Scan(&uid)
	if err != nil {
		http.Error(w, "link expired", 400)
		return
	}
	s.setSession(w, uid)
	http.Redirect(w, r, "/dash", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("pdt"); err == nil {
		if pool, e := s.db(); e == nil {
			_, _ = pool.Exec(context.Background(), `DELETE FROM sessions WHERE token=$1`, c.Value)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "pdt", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) need(w http.ResponseWriter, r *http.Request, roles ...string) *db.User {
	u := s.user(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil
	}
	if len(roles) == 0 {
		return u
	}
	for _, role := range roles {
		if u.Role == role || u.Role == "admin" {
			return u
		}
	}
	http.Error(w, "forbidden", 403)
	return nil
}

func (s *Server) dash(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r)
	if u == nil {
		return
	}
	p := s.base(r, "Dashboard")
	pool, _ := s.db()
	type item struct {
		ID                int64
		Title, Kind, When string
	}
	data := map[string]any{"Role": u.Role}
	if u.Role == "consumer" {
		rows, _ := pool.Query(context.Background(),
			`SELECT p.id, p.title, p.published_at::text FROM pieces p
			 JOIN follows f ON f.author_id=p.author_id
			 WHERE f.consumer_id=$1 AND p.status='live' ORDER BY p.published_at DESC LIMIT 50`, u.ID)
		var list []item
		for rows.Next() {
			var it item
			_ = rows.Scan(&it.ID, &it.Title, &it.When)
			it.Kind = "piece"
			list = append(list, it)
		}
		rows.Close()
		data["Feed"] = list
	} else {
		rows, _ := pool.Query(context.Background(),
			`SELECT id, title, type, coalesce(published_at::text, updated_at::text) FROM pieces WHERE author_id=$1 ORDER BY updated_at DESC LIMIT 40`, u.ID)
		var list []item
		for rows.Next() {
			var it item
			_ = rows.Scan(&it.ID, &it.Title, &it.Kind, &it.When)
			list = append(list, it)
		}
		rows.Close()
		data["Pieces"] = list
		var sites []map[string]any
		srows, _ := pool.Query(context.Background(),
			`SELECT s.id, s.name, s.slug FROM sites s JOIN site_members m ON m.site_id=s.id WHERE m.user_id=$1 ORDER BY s.is_main DESC, s.name`, u.ID)
		for srows.Next() {
			var id int64
			var name, slug string
			_ = srows.Scan(&id, &name, &slug)
			sites = append(sites, map[string]any{"ID": id, "Name": name, "Slug": slug})
		}
		srows.Close()
		data["Sites"] = sites
		data["ManySites"] = len(sites) > 1
	}
	p.Data = data
	s.render(w, "dash.html", p)
}

func (s *Server) dashBio(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "author", "admin")
	if u == nil {
		return
	}
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		handle := strings.TrimSpace(r.FormValue("handle"))
		var hp *string
		if handle != "" && s.cfg.Multi() {
			hp = &handle
		}
		_, _ = pool.Exec(context.Background(),
			`UPDATE users SET name=$1, bio=$2, handle=$3 WHERE id=$4`,
			r.FormValue("name"), r.FormValue("bio"), hp, u.ID)
		http.Redirect(w, r, "/dash/bio", http.StatusSeeOther)
		return
	}
	u, _ = db.UserByID(pool, u.ID)
	p := s.base(r, "Bio")
	p.Data = u
	s.render(w, "bio-dash.html", p)
}

func (s *Server) edit(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "author", "admin")
	if u == nil {
		return
	}
	pool, _ := s.db()
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		title := r.FormValue("title")
		slug := slugify(val(r, "slug", title))
		typ := val(r, "type", "post")
		content := r.FormValue("content")
		excerpt := r.FormValue("excerpt")
		status := val(r, "status", "draft")
		siteID, _ := strconv.ParseInt(val(r, "site_id", "0"), 10, 64)
		if siteID == 0 {
			_ = pool.QueryRow(context.Background(), `SELECT id FROM sites WHERE is_main=true`).Scan(&siteID)
		}
		seriesID := nullInt(r.FormValue("series_id"))
		featPlace := val(r, "feat_place", "top")
		var pub any
		if status == "live" {
			pub = time.Now()
		}
		if id == 0 {
			err := pool.QueryRow(context.Background(),
				`INSERT INTO pieces(site_id,author_id,type,status,series_id,title,slug,content,excerpt,feat_img,feat_aud,feat_vid,feat_place,published_at)
				 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`,
				siteID, u.ID, typ, status, seriesID, title, slug, content, excerpt,
				nullStr(r.FormValue("feat_img")), nullStr(r.FormValue("feat_aud")), nullStr(r.FormValue("feat_vid")),
				featPlace, pub).Scan(&id)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		} else {
			_, err := pool.Exec(context.Background(),
				`UPDATE pieces SET title=$1,slug=$2,type=$3,status=$4,series_id=$5,content=$6,excerpt=$7,
				 feat_img=$8,feat_aud=$9,feat_vid=$10,feat_place=$11,published_at=COALESCE($12,published_at),updated_at=now()
				 WHERE id=$13 AND (author_id=$14 OR $15='admin')`,
				title, slug, typ, status, seriesID, content, excerpt,
				nullStr(r.FormValue("feat_img")), nullStr(r.FormValue("feat_aud")), nullStr(r.FormValue("feat_vid")),
				featPlace, pub, id, u.ID, u.Role)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		http.Redirect(w, r, "/dash/edit?id="+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	row := map[string]any{"ID": id, "Type": "post", "Status": "draft", "FeatPlace": "top"}
	if id > 0 {
		var pid, seriesID, siteID int64
		var title, slug, typ, status, content, excerpt, featImg, featAud, featVid, featPlace string
		_ = pool.QueryRow(context.Background(),
			`SELECT id,title,slug,type,status,content,excerpt,coalesce(feat_img,''),coalesce(feat_aud,''),coalesce(feat_vid,''),feat_place,coalesce(series_id,0),site_id
			 FROM pieces WHERE id=$1`, id).Scan(
			&pid, &title, &slug, &typ, &status, &content, &excerpt,
			&featImg, &featAud, &featVid, &featPlace, &seriesID, &siteID)
		row = map[string]any{"ID": pid, "Title": title, "Slug": slug, "Type": typ, "Status": status, "Content": content, "Excerpt": excerpt, "FeatImg": featImg, "FeatAud": featAud, "FeatVid": featVid, "FeatPlace": featPlace, "SeriesID": seriesID, "SiteID": siteID}
	}
	var sites, series, classes []map[string]any
	srows, _ := pool.Query(context.Background(),
		`SELECT s.id,s.name FROM sites s JOIN site_members m ON m.site_id=s.id WHERE m.user_id=$1`, u.ID)
	for srows.Next() {
		var id int64
		var n string
		_ = srows.Scan(&id, &n)
		sites = append(sites, map[string]any{"ID": id, "Name": n})
	}
	srows.Close()
	r2, _ := pool.Query(context.Background(), `SELECT id,name FROM series ORDER BY name`)
	for r2.Next() {
		var id int64
		var n string
		_ = r2.Scan(&id, &n)
		series = append(series, map[string]any{"ID": id, "Name": n})
	}
	r2.Close()
	r3, _ := pool.Query(context.Background(), `SELECT name,css FROM style_classes`)
	for r3.Next() {
		var n, c string
		_ = r3.Scan(&n, &c)
		classes = append(classes, map[string]any{"Name": n, "CSS": c})
	}
	r3.Close()
	p := s.base(r, "Compose")
	p.Data = map[string]any{"Piece": row, "Sites": sites, "ManySites": len(sites) > 1, "Series": series, "Classes": classes}
	s.render(w, "editor.html", p)
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = tok(4)
	}
	return out
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullInt(s string) any {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n == 0 {
		return nil
	}
	return n
}

func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "author", "admin")
	if u == nil {
		return
	}
	dir := filepath.Join(s.root, "var", "media")
	os.MkdirAll(dir, 0755)
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		f, hdr, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		defer f.Close()
		name := fmt.Sprintf("%d-%s", time.Now().Unix(), filepath.Base(hdr.Filename))
		dst, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		io.Copy(dst, f)
		dst.Close()
		kind := "doc"
		mt := hdr.Header.Get("Content-Type")
		if strings.HasPrefix(mt, "image/") {
			kind = "image"
		} else if strings.HasPrefix(mt, "audio/") {
			kind = "audio"
		} else if strings.HasPrefix(mt, "video/") {
			kind = "video"
		}
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO media(user_id,kind,path,title,mime) VALUES($1,$2,$3,$4,$5)`,
			u.ID, kind, "/media/"+name, hdr.Filename, mt)
		http.Redirect(w, r, "/dash/media", http.StatusSeeOther)
		return
	}
	s.muxMediaOnce()
	p := s.base(r, "Media")
	rows, _ := pool.Query(context.Background(), `SELECT id,kind,path,title FROM media ORDER BY id DESC LIMIT 80`)
	var list []map[string]any
	for rows.Next() {
		var id int64
		var k, path, title string
		_ = rows.Scan(&id, &k, &path, &title)
		list = append(list, map[string]any{"ID": id, "Kind": k, "Path": path, "Title": title})
	}
	rows.Close()
	p.Data = list
	s.render(w, "media.html", p)
}

func (s *Server) muxMediaOnce() {
	s.mediaOnce.Do(func() {
		s.mux.Handle("/media/", http.StripPrefix("/media/", http.FileServer(http.Dir(filepath.Join(s.root, "var", "media")))))
	})
}

func (s *Server) series(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "author", "admin")
	if u == nil {
		return
	}
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		var sid int64
		_ = pool.QueryRow(context.Background(), `SELECT id FROM sites WHERE is_main=true`).Scan(&sid)
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO series(site_id,name,slug,descr,itunes_cat) VALUES($1,$2,$3,$4,$5)`,
			sid, r.FormValue("name"), slugify(r.FormValue("name")), r.FormValue("descr"), r.FormValue("itunes_cat"))
		http.Redirect(w, r, "/dash/series", http.StatusSeeOther)
		return
	}
	p := s.base(r, "Series")
	rows, _ := pool.Query(context.Background(), `SELECT id,name,slug FROM series ORDER BY name`)
	var list []map[string]any
	for rows.Next() {
		var id int64
		var n, sl string
		_ = rows.Scan(&id, &n, &sl)
		list = append(list, map[string]any{"ID": id, "Name": n, "Slug": sl})
	}
	rows.Close()
	p.Data = list
	s.render(w, "series.html", p)
}

func (s *Server) agg(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "admin")
	if u == nil {
		return
	}
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		var sid int64
		_ = pool.QueryRow(context.Background(), `SELECT id FROM sites WHERE is_main=true`).Scan(&sid)
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO aggregation(site_id,name,source,interval_min) VALUES($1,$2,$3,$4)`,
			sid, r.FormValue("name"), r.FormValue("source"), atoi(val(r, "interval", "15")))
		http.Redirect(w, r, "/dash/feeds", http.StatusSeeOther)
		return
	}
	p := s.base(r, "Aggregator")
	rows, _ := pool.Query(context.Background(), `SELECT id,name,source,status FROM aggregation ORDER BY id DESC`)
	var list []map[string]any
	for rows.Next() {
		var id int64
		var n, src, st string
		_ = rows.Scan(&id, &n, &src, &st)
		list = append(list, map[string]any{"ID": id, "Name": n, "Source": src, "Status": st})
	}
	rows.Close()
	p.Data = list
	s.render(w, "agg.html", p)
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func (s *Server) dashShop(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "admin", "author")
	if u == nil {
		return
	}
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		var sid int64
		_ = pool.QueryRow(context.Background(), `SELECT id FROM sites WHERE is_main=true`).Scan(&sid)
		price := int(atof(r.FormValue("price")) * 100)
		kind := val(r, "kind", "physical")
		var n any
		var unit any
		if kind == "subscription" {
			n = atoi(val(r, "interval_n", "1"))
			unit = val(r, "interval_unit", "month")
		}
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO products(site_id,title,slug,body,price_cents,kind,interval_n,interval_unit,ship,virtual_url)
			 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			sid, r.FormValue("title"), slugify(r.FormValue("title")), r.FormValue("body"),
			price, kind, n, unit, r.FormValue("ship") == "1", nullStr(r.FormValue("virtual_url")))
		http.Redirect(w, r, "/dash/shop", http.StatusSeeOther)
		return
	}
	p := s.base(r, "Products")
	rows, _ := pool.Query(context.Background(), `SELECT id,title,price_cents,kind,slug FROM products ORDER BY id DESC`)
	var list []map[string]any
	for rows.Next() {
		var id int64
		var t, k, sl string
		var pc int
		_ = rows.Scan(&id, &t, &pc, &k, &sl)
		list = append(list, map[string]any{"ID": id, "Title": t, "Price": fmt.Sprintf("%.2f", float64(pc)/100), "Kind": k, "Slug": sl})
	}
	rows.Close()
	p.Data = list
	s.render(w, "dash-shop.html", p)
}

func atof(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }

func (s *Server) dashWallet(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "admin")
	if u == nil {
		return
	}
	p := s.base(r, "Wallet")
	p.Data = wallet.List(s.cfg)
	s.render(w, "wallet.html", p)
}

func (s *Server) security(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r)
	if u == nil {
		return
	}
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		if r.FormValue("totp_start") == "1" {
			sec := totp.Secret()
			_, _ = pool.Exec(context.Background(), `UPDATE users SET totp_secret=$1, totp_enabled=false WHERE id=$2`, sec, u.ID)
		}
		if r.FormValue("totp_confirm") != "" && u.TOTP != nil {
			if totp.Verify(*u.TOTP, r.FormValue("code")) {
				_, _ = pool.Exec(context.Background(), `UPDATE users SET totp_enabled=true WHERE id=$1`, u.ID)
			}
		}
		http.Redirect(w, r, "/dash/security", http.StatusSeeOther)
		return
	}
	u, _ = db.UserByID(pool, u.ID)
	p := s.base(r, "Security")
	uri := ""
	if u.TOTP != nil {
		uri = totp.URI(*u.TOTP, u.LoginID, "pdt-news")
	}
	p.Data = map[string]any{"User": u, "URI": uri}
	s.render(w, "security.html", p)
}

func (s *Server) shop(w http.ResponseWriter, r *http.Request) {
	pool, err := s.db()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	p := s.base(r, "Shop")
	rows, _ := pool.Query(context.Background(), `SELECT id,title,slug,body,price_cents,kind FROM products WHERE active=true ORDER BY id DESC`)
	var list []map[string]any
	for rows.Next() {
		var id int64
		var t, sl, body, k string
		var pc int
		_ = rows.Scan(&id, &t, &sl, &body, &pc, &k)
		list = append(list, map[string]any{"ID": id, "Title": t, "Slug": sl, "Body": body, "Price": fmt.Sprintf("%.2f", float64(pc)/100), "Kind": k})
	}
	rows.Close()
	p.Data = list
	s.render(w, "shop.html", p)
}

func (s *Server) checkout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/shop", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	pool, _ := s.db()
	pid, _ := strconv.ParseInt(r.FormValue("product_id"), 10, 64)
	var title string
	var price int
	var kind string
	var n *int
	var unit *string
	err := pool.QueryRow(context.Background(),
		`SELECT title,price_cents,kind,interval_n,interval_unit FROM products WHERE id=$1`, pid).
		Scan(&title, &price, &kind, &n, &unit)
	if err != nil {
		http.Error(w, "product", 404)
		return
	}
	st := r.FormValue("state")
	zip := r.FormValue("zip")
	taxc := tax.Cents(price, "US", st, zip)
	email := r.FormValue("email")
	var uid *int64
	if u := s.user(r); u != nil {
		uid = &u.ID
		email = u.Email
	}
	var oid int64
	_ = pool.QueryRow(context.Background(),
		`INSERT INTO orders(user_id,email,total_cents,tax_cents,status,dest_state,dest_zip,pay_via)
		 VALUES($1,$2,$3,$4,'paid',$5,$6,$7) RETURNING id`,
		uid, email, price+taxc, taxc, st, zip, val(r, "pay_via", "invoice")).Scan(&oid)
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO order_items(order_id,product_id,title,qty,price_cents) VALUES($1,$2,$3,1,$4)`,
		oid, pid, title, price)
	num := fmt.Sprintf("INV-%d", oid)
	pdf := simplePDF(num, title, price, taxc)
	pdfPath := filepath.Join(s.root, "var", "invoices")
	os.MkdirAll(pdfPath, 0755)
	fn := filepath.Join(pdfPath, num+".pdf")
	_ = os.WriteFile(fn, pdf, 0644)
	_, _ = pool.Exec(context.Background(), `INSERT INTO invoices(order_id,number,pdf_path) VALUES($1,$2,$3)`, oid, num, fn)
	if kind == "subscription" && uid != nil && n != nil && unit != nil {
		start := r.FormValue("start_on")
		if start == "" {
			start = time.Now().Format("2006-01-02")
		}
		skip := nullStr(r.FormValue("skip_until"))
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO subscriptions(user_id,product_id,interval_n,interval_unit,start_on,skip_until,next_on)
			 VALUES($1,$2,$3,$4,$5,$6,$5::date)`, *uid, pid, *n, *unit, start, skip)
	}
	if uid != nil {
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO entitlements(user_id,product_id,source) VALUES($1,$2,'purchase') ON CONFLICT DO NOTHING`, *uid, pid)
	}
	if email != "" {
		_ = mailer.SendPDF(s.cfg, email, "Invoice "+num, "Thank you.\n\nTotal: $"+fmt.Sprintf("%.2f", float64(price+taxc)/100)+"\n", pdf, num+".pdf")
	}
	p := s.base(r, "Receipt")
	p.Flash = "Order " + num + " recorded. Invoice emailed if mail is on."
	s.render(w, "receipt.html", p)
}

func simplePDF(num, title string, price, taxc int) []byte {
	// Minimal one-page PDF.
	esc := func(s string) string {
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "(", "\\(")
		s = strings.ReplaceAll(s, ")", "\\)")
		return s
	}
	text := fmt.Sprintf("BT /F1 18 Tf 72 720 Td (%s) Tj T* /F1 12 Tf (%s) Tj T* (Item: %s) Tj T* (Subtotal: $%.2f) Tj T* (Tax: $%.2f) Tj T* (Total: $%.2f) Tj ET",
		esc(num), esc("pdt-news invoice"), esc(title), float64(price)/100, float64(taxc)/100, float64(price+taxc)/100)
	stream := text
	body := fmt.Sprintf("%%PDF-1.1\n1 0 obj<< /Type /Catalog /Pages 2 0 R >>endobj\n2 0 obj<< /Type /Pages /Kids [3 0 R] /Count 1 >>endobj\n3 0 obj<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>endobj\n4 0 obj<< /Length %d >>stream\n%s\nendstream\nendobj\n5 0 obj<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>endobj\nxref\n0 6\n0000000000 65535 f \ntrailer<< /Size 6 /Root 1 0 R >>\nstartxref\n0\n%%%%EOF", len(stream), stream)
	return []byte(body)
}

func (s *Server) feed(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pool, err := s.db()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		series := r.URL.Query().Get("series")
		q := `SELECT p.title,p.slug,p.excerpt,p.content,p.published_at,coalesce(u.handle,''),coalesce(p.feat_aud,''),coalesce(p.feat_vid,'')
		      FROM pieces p JOIN users u ON u.id=p.author_id WHERE p.status='live' AND p.type='post'`
		args := []any{}
		if series != "" {
			q += ` AND p.series_id = (SELECT id FROM series WHERE slug=$1 LIMIT 1)`
			args = append(args, series)
		}
		q += ` ORDER BY p.published_at DESC LIMIT 50`
		rows, err := pool.Query(context.Background(), q, args...)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		var items []map[string]string
		for rows.Next() {
			var title, slug, ex, content, pub, handle, aud, vid string
			var pubt *time.Time
			_ = rows.Scan(&title, &slug, &ex, &content, &pubt, &handle, &aud, &vid)
			if pubt != nil {
				pub = pubt.Format(time.RFC1123Z)
			}
			link := s.cfg.URL + "/p/" + slug
			items = append(items, map[string]string{"Title": title, "Link": link, "Excerpt": ex, "Content": content, "Pub": pub, "Handle": handle, "Aud": aud, "Vid": vid})
		}
		if kind == "atom" {
			w.Header().Set("Content-Type", "application/atom+xml")
			fmt.Fprintf(w, `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>pdt-news</title>`)
			for _, it := range items {
				fmt.Fprintf(w, `<entry><title>%s</title><link href="%s"/><updated>%s</updated><summary>%s</summary></entry>`,
					xmlEsc(it["Title"]), xmlEsc(it["Link"]), xmlEsc(it["Pub"]), xmlEsc(it["Excerpt"]))
			}
			fmt.Fprint(w, `</feed>`)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><title>pdt-news</title><link>%s</link>`, xmlEsc(s.cfg.URL))
		for _, it := range items {
			fmt.Fprintf(w, `<item><title>%s</title><link>%s</link><pubDate>%s</pubDate><description>%s</description>`,
				xmlEsc(it["Title"]), xmlEsc(it["Link"]), xmlEsc(it["Pub"]), xmlEsc(it["Excerpt"]))
			if it["Aud"] != "" {
				fmt.Fprintf(w, `<enclosure url="%s" type="audio/mpeg"/>`, xmlEsc(it["Aud"]))
			}
			fmt.Fprint(w, `</item>`)
		}
		fmt.Fprint(w, `</channel></rss>`)
	}
}

func xmlEsc(s string) string {
	s = strings.ReplaceAll(s, "&", "&")
	s = strings.ReplaceAll(s, "<", "<")
	s = strings.ReplaceAll(s, ">", ">")
	return s
}

func (s *Server) pub(w http.ResponseWriter, r *http.Request) {
	pth := strings.Trim(r.URL.Path, "/")
	if strings.HasPrefix(pth, "series/") && strings.HasSuffix(pth, "/rss") {
		r.URL.RawQuery = "series=" + strings.TrimSuffix(strings.TrimPrefix(pth, "series/"), "/rss")
		s.feed("rss")(w, r)
		return
	}
	if strings.HasPrefix(pth, "series/") && (strings.HasSuffix(pth, "/atom") || strings.HasSuffix(pth, "/feed")) {
		kind := "rss"
		if strings.HasSuffix(pth, "/atom") {
			kind = "atom"
		}
		slug := strings.TrimPrefix(pth, "series/")
		slug = strings.TrimSuffix(slug, "/rss")
		slug = strings.TrimSuffix(slug, "/atom")
		slug = strings.TrimSuffix(slug, "/feed")
		r.URL.RawQuery = "series=" + slug
		s.feed(kind)(w, r)
		return
	}
	pool, err := s.db()
	if err != nil {
		s.render(w, "home.html", s.base(r, "pdt-news"))
		return
	}
	if !db.Installed(pool) && pth == "" {
		http.Redirect(w, r, "/install", http.StatusSeeOther)
		return
	}
	if pth == "" {
		s.home(w, r)
		return
	}
	if strings.HasPrefix(pth, "p/") {
		s.piece(w, r, strings.TrimPrefix(pth, "p/"))
		return
	}
	if strings.HasPrefix(pth, "series/") {
		s.seriesPub(w, r, strings.TrimPrefix(pth, "series/"))
		return
	}
	if s.cfg.Multi() && strings.HasPrefix(pth, "@") {
		s.bio(w, r, strings.TrimPrefix(pth, "@"))
		return
	}
	if s.cfg.Multi() && !strings.Contains(pth, "/") {
		s.authorBlog(w, r, pth)
		return
	}
	// page by slug
	s.piece(w, r, pth)
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	pool, _ := s.db()
	p := s.base(r, "")
	rows, _ := pool.Query(context.Background(),
		`SELECT p.id,p.title,p.slug,p.excerpt,coalesce(u.handle,''),u.name,coalesce(s.slug,''),coalesce(s.name,'')
		 FROM pieces p JOIN users u ON u.id=p.author_id LEFT JOIN series s ON s.id=p.series_id
		 WHERE p.status='live' AND p.type='post' ORDER BY p.published_at DESC LIMIT 30`)
	var list []map[string]any
	for rows.Next() {
		var id int64
		var title, slug, ex, handle, name, sslug, sname string
		_ = rows.Scan(&id, &title, &slug, &ex, &handle, &name, &sslug, &sname)
		list = append(list, map[string]any{"ID": id, "Title": title, "Slug": slug, "Excerpt": ex, "Handle": handle, "Name": name, "Series": sname, "SeriesSlug": sslug})
	}
	rows.Close()
	p.Data = list
	s.render(w, "home.html", p)
}

func (s *Server) piece(w http.ResponseWriter, r *http.Request, slug string) {
	pool, _ := s.db()
	var id int64
	var title, pslug, content, excerpt, featPlace, featImg, featAud, featVid, name, handle, when, sname, sslug string
	err := pool.QueryRow(context.Background(),
		`SELECT p.id,p.title,p.slug,p.content,p.excerpt,p.feat_place,coalesce(p.feat_img,''),coalesce(p.feat_aud,''),coalesce(p.feat_vid,''),
		        u.name,coalesce(u.handle,''),coalesce(p.published_at::text,''),coalesce(ser.name,''),coalesce(ser.slug,'')
		 FROM pieces p JOIN users u ON u.id=p.author_id LEFT JOIN series ser ON ser.id=p.series_id
		 WHERE p.slug=$1 AND p.status='live'`, slug).Scan(
		&id, &title, &pslug, &content, &excerpt, &featPlace, &featImg, &featAud, &featVid, &name, &handle, &when, &sname, &sslug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	row := map[string]any{"ID": id, "Title": title, "Slug": pslug, "Content": template.HTML(content), "Excerpt": excerpt, "FeatPlace": featPlace, "FeatImg": featImg, "FeatAud": featAud, "FeatVid": featVid, "Name": name, "Handle": handle, "When": when, "Series": sname, "SeriesSlug": sslug}
	p := s.base(r, title)
	p.Data = row
	s.render(w, "piece.html", p)
}

func (s *Server) bio(w http.ResponseWriter, r *http.Request, handle string) {
	pool, _ := s.db()
	u, err := db.UserByHandle(pool, handle)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p := s.base(r, u.Name)
	p.Data = u
	s.render(w, "bio.html", p)
}

func (s *Server) authorBlog(w http.ResponseWriter, r *http.Request, handle string) {
	pool, _ := s.db()
	u, err := db.UserByHandle(pool, handle)
	if err != nil {
		s.piece(w, r, handle)
		return
	}
	rows, _ := pool.Query(context.Background(),
		`SELECT title,slug,excerpt FROM pieces WHERE author_id=$1 AND status='live' AND type='post' ORDER BY published_at DESC LIMIT 40`, u.ID)
	var list []map[string]any
	for rows.Next() {
		var t, sl, ex string
		_ = rows.Scan(&t, &sl, &ex)
		list = append(list, map[string]any{"Title": t, "Slug": sl, "Excerpt": ex})
	}
	rows.Close()
	p := s.base(r, u.Name)
	p.Data = map[string]any{"Author": u, "Pieces": list}
	s.render(w, "author.html", p)
}

func (s *Server) seriesPub(w http.ResponseWriter, r *http.Request, slug string) {
	pool, _ := s.db()
	var name string
	var id int64
	if err := pool.QueryRow(context.Background(), `SELECT id,name FROM series WHERE slug=$1`, slug).Scan(&id, &name); err != nil {
		http.NotFound(w, r)
		return
	}
	rows, _ := pool.Query(context.Background(),
		`SELECT title,slug,excerpt FROM pieces WHERE series_id=$1 AND status='live' ORDER BY published_at DESC LIMIT 40`, id)
	var list []map[string]any
	for rows.Next() {
		var t, sl, ex string
		_ = rows.Scan(&t, &sl, &ex)
		list = append(list, map[string]any{"Title": t, "Slug": sl, "Excerpt": ex})
	}
	rows.Close()
	p := s.base(r, name)
	p.Data = map[string]any{"Name": name, "Slug": slug, "Pieces": list}
	s.render(w, "series-pub.html", p)
}
