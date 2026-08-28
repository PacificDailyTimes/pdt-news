package httpx

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) dashCoupons(w http.ResponseWriter, r *http.Request) {
	if !s.flags().Shop.On {
		http.NotFound(w, r)
		return
	}
	u := s.need(w, r, "admin")
	if u == nil {
		return
	}
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		var sid int64
		_ = pool.QueryRow(context.Background(), `SELECT id FROM sites WHERE is_main=true`).Scan(&sid)
		amt, _ := strconv.Atoi(r.FormValue("amount"))
		minc, _ := strconv.Atoi(r.FormValue("min_cents"))
		code := strings.ToUpper(strings.TrimSpace(r.FormValue("code")))
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO coupons(site_id,code,kind,amount,min_cents,max_uses,expires) VALUES($1,$2,$3,$4,$5,$6,$7)`,
			sid, code, val(r, "kind", "percent"), amt, minc, nullInt(r.FormValue("max_uses")), nullStr(r.FormValue("expires")))
		http.Redirect(w, r, "/dash/coupons", http.StatusSeeOther)
		return
	}
	p := s.base(r, "Coupons")
	s.decorate(&p, "")
	rows, _ := pool.Query(context.Background(),
		`SELECT id,code,kind,amount,used,coalesce(max_uses,0),active FROM coupons ORDER BY id DESC`)
	var list []map[string]any
	for rows.Next() {
		var id int64
		var code, kind string
		var amt, used, maxu int
		var active bool
		_ = rows.Scan(&id, &code, &kind, &amt, &used, &maxu, &active)
		list = append(list, map[string]any{"ID": id, "Code": code, "Kind": kind, "Amount": amt, "Used": used, "Max": maxu, "Active": active})
	}
	rows.Close()
	p.Data = list
	s.render(w, "dash-coupons.html", p)
}

func (s *Server) applyCoupon(code string, total int) (disc int, okCode string) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return 0, ""
	}
	pool, err := s.db()
	if err != nil {
		return 0, ""
	}
	var kind string
	var amount, minc, used int
	var maxu *int
	var expires *time.Time
	var active bool
	err = pool.QueryRow(context.Background(),
		`SELECT kind,amount,min_cents,used,max_uses,expires,active FROM coupons WHERE upper(code)=$1`, code).
		Scan(&kind, &amount, &minc, &used, &maxu, &expires, &active)
	if err != nil || !active {
		return 0, ""
	}
	if maxu != nil && used >= *maxu {
		return 0, ""
	}
	if expires != nil && expires.Before(time.Now()) {
		return 0, ""
	}
	if total < minc {
		return 0, ""
	}
	if kind == "cents" {
		disc = amount
	} else {
		disc = total * amount / 100
	}
	if disc > total {
		disc = total
	}
	if disc < 0 {
		disc = 0
	}
	return disc, code
}

func (s *Server) bumpCoupon(code string) {
	if code == "" {
		return
	}
	pool, err := s.db()
	if err != nil {
		return
	}
	_, _ = pool.Exec(context.Background(), `UPDATE coupons SET used=used+1 WHERE upper(code)=upper($1)`, code)
}
