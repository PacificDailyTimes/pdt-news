package httpx

import (
	"context"
	"html/template"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) dashScroll(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "admin", "author")
	if u == nil {
		return
	}
	pool, _ := s.db()
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		var sid int64
		_ = pool.QueryRow(context.Background(), `SELECT id FROM sites WHERE is_main=true`).Scan(&sid)
		min, _ := strconv.Atoi(r.FormValue("min_tier"))
		if id == 0 {
			_ = pool.QueryRow(context.Background(),
				`INSERT INTO scrolls(site_id,author_id,title,slug,status,min_tier) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`,
				sid, u.ID, r.FormValue("title"), slugify(val(r, "slug", r.FormValue("title"))), val(r, "status", "draft"), min).Scan(&id)
		} else {
			_, _ = pool.Exec(context.Background(),
				`UPDATE scrolls SET title=$1,slug=$2,status=$3,min_tier=$4 WHERE id=$5`,
				r.FormValue("title"), slugify(val(r, "slug", r.FormValue("title"))), val(r, "status", "draft"), min, id)
		}
		if r.FormValue("add_piece") != "" {
			pid, _ := strconv.ParseInt(r.FormValue("add_piece"), 10, 64)
			islug := r.FormValue("item_slug")
			if islug == "" {
				_ = pool.QueryRow(context.Background(), `SELECT slug FROM pieces WHERE id=$1`, pid).Scan(&islug)
			}
			_, _ = pool.Exec(context.Background(),
				`INSERT INTO scroll_items(scroll_id,piece_id,slug,pos) VALUES($1,$2,$3,coalesce((SELECT max(pos)+1 FROM scroll_items WHERE scroll_id=$1),0))`,
				id, pid, slugify(islug))
		}
		if r.FormValue("add_form") != "" {
			fid, _ := strconv.ParseInt(r.FormValue("add_form"), 10, 64)
			islug := r.FormValue("item_slug")
			if islug == "" {
				_ = pool.QueryRow(context.Background(), `SELECT slug FROM forms WHERE id=$1`, fid).Scan(&islug)
			}
			_, _ = pool.Exec(context.Background(),
				`INSERT INTO scroll_items(scroll_id,form_id,slug,pos) VALUES($1,$2,$3,coalesce((SELECT max(pos)+1 FROM scroll_items WHERE scroll_id=$1),0))`,
				id, fid, slugify(islug))
		}
		http.Redirect(w, r, "/dash/scroll?id="+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	p := s.base(r, "Scroll-thru")
	s.decorate(&p, "")
	list := []map[string]any{}
	lr, _ := pool.Query(context.Background(), `SELECT id,title,slug,status FROM scrolls ORDER BY id DESC`)
	for lr.Next() {
		var sid int64
		var t, sl, st string
		_ = lr.Scan(&sid, &t, &sl, &st)
		list = append(list, map[string]any{"ID": sid, "Title": t, "Slug": sl, "Status": st})
	}
	lr.Close()
	cur := map[string]any{"ID": id}
	var items, pieces, forms []map[string]any
	if id > 0 {
		var title, slug, status string
		var min int
		_ = pool.QueryRow(context.Background(), `SELECT title,slug,status,min_tier FROM scrolls WHERE id=$1`, id).Scan(&title, &slug, &status, &min)
		cur = map[string]any{"ID": id, "Title": title, "Slug": slug, "Status": status, "Min": min}
		ir, _ := pool.Query(context.Background(),
			`SELECT id,slug,coalesce(piece_id,0),coalesce(form_id,0) FROM scroll_items WHERE scroll_id=$1 ORDER BY pos,id`, id)
		for ir.Next() {
			var iid, pid, fid int64
			var isl string
			_ = ir.Scan(&iid, &isl, &pid, &fid)
			items = append(items, map[string]any{"ID": iid, "Slug": isl, "Piece": pid, "Form": fid})
		}
		ir.Close()
	}
	pr, _ := pool.Query(context.Background(), `SELECT id,title,type FROM pieces WHERE status='live' AND type IN ('page','landing','post') ORDER BY title`)
	for pr.Next() {
		var pid int64
		var t, typ string
		_ = pr.Scan(&pid, &t, &typ)
		pieces = append(pieces, map[string]any{"ID": pid, "Title": t, "Type": typ})
	}
	pr.Close()
	fr, _ := pool.Query(context.Background(), `SELECT id,name FROM forms ORDER BY name`)
	for fr.Next() {
		var fid int64
		var n string
		_ = fr.Scan(&fid, &n)
		forms = append(forms, map[string]any{"ID": fid, "Name": n})
	}
	fr.Close()
	p.Data = map[string]any{"List": list, "Cur": cur, "Items": items, "Pieces": pieces, "Forms": forms}
	s.render(w, "dash-scroll.html", p)
}

func (s *Server) pubScroll(w http.ResponseWriter, r *http.Request, scrollSlug, itemSlug string) {
	pool, _ := s.db()
	var sid int64
	var title string
	var min int
	if err := pool.QueryRow(context.Background(),
		`SELECT id,title,min_tier FROM scrolls WHERE slug=$1 AND status='live'`, scrollSlug).Scan(&sid, &title, &min); err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.gate(w, r, min, title) {
		return
	}
	rows, _ := pool.Query(context.Background(),
		`SELECT i.slug, coalesce(p.title,f.name,''), coalesce(p.content,''), coalesce(p.type,''), coalesce(p.landing::text,''),
		        i.form_id, coalesce(p.feat_img,''), coalesce(p.excerpt,'')
		 FROM scroll_items i
		 LEFT JOIN pieces p ON p.id=i.piece_id
		 LEFT JOIN forms f ON f.id=i.form_id
		 WHERE i.scroll_id=$1 ORDER BY i.pos, i.id`, sid)
	var items []map[string]any
	for rows.Next() {
		var sl, t, content, typ, landing, feat, excerpt string
		var fid *int64
		_ = rows.Scan(&sl, &t, &content, &typ, &landing, &fid, &feat, &excerpt)
		items = append(items, map[string]any{
			"Slug": sl, "Title": t, "Content": template.HTML(content), "Type": typ,
			"Landing": landing, "FormID": fid, "Feat": feat, "Excerpt": excerpt,
		})
	}
	rows.Close()
	if itemSlug == "" && len(items) > 0 {
		itemSlug, _ = items[0]["Slug"].(string)
	}
	p := s.base(r, title)
	s.decorate(&p, "landing")
	p.Canonical = strings.TrimRight(s.cfg.URL, "/") + "/" + scrollSlug + "/" + itemSlug
	p.Data = map[string]any{"Title": title, "Slug": scrollSlug, "Item": itemSlug, "Items": items}
	s.render(w, "scroll.html", p)
}
