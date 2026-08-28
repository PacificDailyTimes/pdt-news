package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/PacificDailyTimes/pdt-news/internal/pay"
	"github.com/PacificDailyTimes/pdt-news/internal/tax"
)

func (s *Server) cartID(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie("pdt_cart")
	if err == nil && c.Value != "" {
		return c.Value
	}
	id := tok(16)
	http.SetCookie(w, &http.Cookie{Name: "pdt_cart", Value: id, Path: "/", MaxAge: 86400 * 90, SameSite: http.SameSiteLaxMode})
	pool, _ := s.db()
	if pool != nil {
		var uid *int64
		if u := s.user(r); u != nil {
			uid = &u.ID
		}
		_, _ = pool.Exec(context.Background(), `INSERT INTO carts(id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, uid)
	}
	return id
}

func (s *Server) dashDept(w http.ResponseWriter, r *http.Request) {
	if !s.flags().Shop.On {
		http.NotFound(w, r)
		return
	}
	u := s.need(w, r, "admin", "author")
	if u == nil {
		return
	}
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		var sid int64
		_ = pool.QueryRow(context.Background(), `SELECT id FROM sites WHERE is_main=true`).Scan(&sid)
		min, _ := strconv.Atoi(r.FormValue("min_tier"))
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO departments(site_id,name,slug,descr,min_tier) VALUES($1,$2,$3,$4,$5)`,
			sid, r.FormValue("name"), slugify(val(r, "slug", r.FormValue("name"))), r.FormValue("descr"), min)
		http.Redirect(w, r, "/dash/dept", http.StatusSeeOther)
		return
	}
	p := s.base(r, "Departments")
	s.decorate(&p, "")
	rows, _ := pool.Query(context.Background(), `SELECT id,name,slug FROM departments ORDER BY name`)
	var list []map[string]any
	for rows.Next() {
		var id int64
		var n, sl string
		_ = rows.Scan(&id, &n, &sl)
		list = append(list, map[string]any{"ID": id, "Name": n, "Slug": sl})
	}
	rows.Close()
	p.Data = list
	s.render(w, "dash-dept.html", p)
}

func (s *Server) dashCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.flags().Shop.On {
		http.NotFound(w, r)
		return
	}
	u := s.need(w, r, "admin", "author")
	if u == nil {
		return
	}
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		var sid int64
		_ = pool.QueryRow(context.Background(), `SELECT id FROM sites WHERE is_main=true`).Scan(&sid)
		min, _ := strconv.Atoi(r.FormValue("min_tier"))
		var cid int64
		_ = pool.QueryRow(context.Background(),
			`INSERT INTO catalogs(site_id,name,slug,body,min_tier) VALUES($1,$2,$3,$4,$5) RETURNING id`,
			sid, r.FormValue("name"), slugify(val(r, "slug", r.FormValue("name"))), r.FormValue("body"), min).Scan(&cid)
		for _, pid := range r.Form["product_id"] {
			id, _ := strconv.ParseInt(pid, 10, 64)
			if id > 0 {
				_, _ = pool.Exec(context.Background(), `INSERT INTO catalog_items(catalog_id,product_id) VALUES($1,$2)`, cid, id)
			}
		}
		for _, did := range r.Form["department_id"] {
			id, _ := strconv.ParseInt(did, 10, 64)
			if id > 0 {
				_, _ = pool.Exec(context.Background(), `INSERT INTO catalog_items(catalog_id,department_id) VALUES($1,$2)`, cid, id)
			}
		}
		http.Redirect(w, r, "/dash/catalog", http.StatusSeeOther)
		return
	}
	p := s.base(r, "Product indexes")
	s.decorate(&p, "")
	prods, depts, cats := []map[string]any{}, []map[string]any{}, []map[string]any{}
	pr, _ := pool.Query(context.Background(), `SELECT id,title FROM products ORDER BY title`)
	for pr.Next() {
		var id int64
		var t string
		_ = pr.Scan(&id, &t)
		prods = append(prods, map[string]any{"ID": id, "Title": t})
	}
	pr.Close()
	dr, _ := pool.Query(context.Background(), `SELECT id,name FROM departments ORDER BY name`)
	for dr.Next() {
		var id int64
		var n string
		_ = dr.Scan(&id, &n)
		depts = append(depts, map[string]any{"ID": id, "Name": n})
	}
	dr.Close()
	cr, _ := pool.Query(context.Background(), `SELECT id,name,slug FROM catalogs ORDER BY name`)
	for cr.Next() {
		var id int64
		var n, sl string
		_ = cr.Scan(&id, &n, &sl)
		cats = append(cats, map[string]any{"ID": id, "Name": n, "Slug": sl})
	}
	cr.Close()
	p.Data = map[string]any{"Products": prods, "Depts": depts, "List": cats}
	s.render(w, "dash-catalog.html", p)
}

func (s *Server) dashTiers(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "admin")
	if u == nil {
		return
	}
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		var sid int64
		_ = pool.QueryRow(context.Background(), `SELECT id FROM sites WHERE is_main=true`).Scan(&sid)
		rank, _ := strconv.Atoi(val(r, "rank", "1"))
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO tiers(site_id,name,slug,rank,product_id) VALUES($1,$2,$3,$4,$5)`,
			sid, r.FormValue("name"), slugify(val(r, "slug", r.FormValue("name"))), rank, nullInt(r.FormValue("product_id")))
		http.Redirect(w, r, "/dash/tiers", http.StatusSeeOther)
		return
	}
	p := s.base(r, "Tiers")
	s.decorate(&p, "")
	rows, _ := pool.Query(context.Background(),
		`SELECT t.id,t.name,t.rank,coalesce(p.title,'') FROM tiers t LEFT JOIN products p ON p.id=t.product_id ORDER BY t.rank`)
	var list []map[string]any
	for rows.Next() {
		var id int64
		var n, pt string
		var rank int
		_ = rows.Scan(&id, &n, &rank, &pt)
		list = append(list, map[string]any{"ID": id, "Name": n, "Rank": rank, "Product": pt})
	}
	rows.Close()
	prods := []map[string]any{}
	pr, _ := pool.Query(context.Background(), `SELECT id,title FROM products WHERE kind='membership' ORDER BY title`)
	for pr.Next() {
		var id int64
		var t string
		_ = pr.Scan(&id, &t)
		prods = append(prods, map[string]any{"ID": id, "Title": t})
	}
	pr.Close()
	p.Data = map[string]any{"List": list, "Products": prods}
	s.render(w, "dash-tiers.html", p)
}

func (s *Server) productPage(w http.ResponseWriter, r *http.Request, slug string) {
	if !s.flags().Shop.On {
		http.NotFound(w, r)
		return
	}
	pool, _ := s.db()
	var id int64
	var title, body, kind, feat string
	var price, min int
	var stock *int
	err := pool.QueryRow(context.Background(),
		`SELECT id,title,body,price_cents,kind,coalesce(features::text,'[]'),min_tier,stock FROM products WHERE slug=$1 AND active=true`, slug).
		Scan(&id, &title, &body, &price, &kind, &feat, &min, &stock)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.gate(w, r, min, title) {
		return
	}
	var features []map[string]any
	_ = json.Unmarshal([]byte(feat), &features)
	p := s.base(r, title)
	s.decorate(&p, "product")
	sold := stock != nil && *stock < 1
	label := ""
	if stock != nil {
		label = strconv.Itoa(*stock) + " in stock"
	}
	p.Data = map[string]any{"ID": id, "Title": title, "Body": body, "Price": fmt.Sprintf("%.2f", float64(price)/100), "Kind": kind, "Features": features, "Slug": slug, "StockLabel": label, "SoldOut": sold}
	s.render(w, "product.html", p)
}

func (s *Server) stockOK(pid int64, qty int) bool {
	pool, err := s.db()
	if err != nil {
		return false
	}
	var stock *int
	if pool.QueryRow(context.Background(), `SELECT stock FROM products WHERE id=$1`, pid).Scan(&stock) != nil {
		return false
	}
	if stock == nil {
		return true
	}
	return *stock >= qty
}

func (s *Server) deptPage(w http.ResponseWriter, r *http.Request, slug string) {
	pool, _ := s.db()
	var id int64
	var name, descr string
	var min int
	if err := pool.QueryRow(context.Background(), `SELECT id,name,descr,min_tier FROM departments WHERE slug=$1`, slug).Scan(&id, &name, &descr, &min); err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.gate(w, r, min, name) {
		return
	}
	rows, _ := pool.Query(context.Background(), `SELECT id,title,slug,price_cents FROM products WHERE department_id=$1 AND active=true`, id)
	var list []map[string]any
	for rows.Next() {
		var pid int64
		var t, sl string
		var pc int
		_ = rows.Scan(&pid, &t, &sl, &pc)
		list = append(list, map[string]any{"ID": pid, "Title": t, "Slug": sl, "Price": fmt.Sprintf("%.2f", float64(pc)/100)})
	}
	rows.Close()
	p := s.base(r, name)
	s.decorate(&p, "department")
	p.Data = map[string]any{"Name": name, "Descr": descr, "Products": list, "Slug": slug}
	s.render(w, "dept.html", p)
}

func (s *Server) catalogPage(w http.ResponseWriter, r *http.Request, slug string) {
	pool, _ := s.db()
	var id int64
	var name, body string
	var min int
	if err := pool.QueryRow(context.Background(), `SELECT id,name,body,min_tier FROM catalogs WHERE slug=$1`, slug).Scan(&id, &name, &body, &min); err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.gate(w, r, min, name) {
		return
	}
	rows, _ := pool.Query(context.Background(),
		`SELECT p.id,p.title,p.slug,p.price_cents FROM catalog_items c JOIN products p ON p.id=c.product_id WHERE c.catalog_id=$1 AND p.active=true`, id)
	var list []map[string]any
	for rows.Next() {
		var pid int64
		var t, sl string
		var pc int
		_ = rows.Scan(&pid, &t, &sl, &pc)
		list = append(list, map[string]any{"ID": pid, "Title": t, "Slug": sl, "Price": fmt.Sprintf("%.2f", float64(pc)/100)})
	}
	rows.Close()
	p := s.base(r, name)
	s.decorate(&p, "shop")
	p.Data = map[string]any{"Name": name, "Body": body, "Products": list}
	s.render(w, "catalog.html", p)
}

func (s *Server) cart(w http.ResponseWriter, r *http.Request) {
	if !s.flags().Shop.On {
		http.NotFound(w, r)
		return
	}
	cid := s.cartID(w, r)
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		act := r.FormValue("act")
		item, _ := strconv.ParseInt(r.FormValue("item"), 10, 64)
		switch act {
		case "add":
			pid, _ := strconv.ParseInt(r.FormValue("product_id"), 10, 64)
			qty, _ := strconv.Atoi(val(r, "qty", "1"))
			if qty < 1 {
				qty = 1
			}
			if !s.stockOK(pid, qty) {
				http.Error(w, "out of stock", 409)
				return
			}
			opts, _ := json.Marshal(r.Form["opt"])
			_, _ = pool.Exec(context.Background(),
				`INSERT INTO carts(id) VALUES($1) ON CONFLICT DO NOTHING`, cid)
			_, _ = pool.Exec(context.Background(),
				`INSERT INTO cart_items(cart_id,product_id,qty,opts) VALUES($1,$2,$3,$4::jsonb)`, cid, pid, qty, string(opts))
		case "qty":
			qty, _ := strconv.Atoi(r.FormValue("qty"))
			if qty < 1 {
				qty = 1
			}
			var pid int64
			_ = pool.QueryRow(context.Background(), `SELECT product_id FROM cart_items WHERE id=$1 AND cart_id=$2`, item, cid).Scan(&pid)
			if pid > 0 && !s.stockOK(pid, qty) {
				http.Error(w, "not enough stock", 409)
				return
			}
			_, _ = pool.Exec(context.Background(), `UPDATE cart_items SET qty=$1 WHERE id=$2 AND cart_id=$3`, qty, item, cid)
		case "coupon":
			_, _ = pool.Exec(context.Background(), `UPDATE carts SET coupon=$1 WHERE id=$2`, strings.ToUpper(strings.TrimSpace(r.FormValue("code"))), cid)
		case "later":
			_, _ = pool.Exec(context.Background(), `UPDATE cart_items SET later=true WHERE id=$1 AND cart_id=$2`, item, cid)
		case "back":
			_, _ = pool.Exec(context.Background(), `UPDATE cart_items SET later=false WHERE id=$1 AND cart_id=$2`, item, cid)
		case "fav":
			_, _ = pool.Exec(context.Background(), `UPDATE cart_items SET fav = NOT fav WHERE id=$1 AND cart_id=$2`, item, cid)
		case "remove":
			_, _ = pool.Exec(context.Background(), `DELETE FROM cart_items WHERE id=$1 AND cart_id=$2`, item, cid)
		case "checkout":
			s.cartCheckout(w, r, cid)
			return
		}
		http.Redirect(w, r, "/cart", http.StatusSeeOther)
		return
	}
	rows, _ := pool.Query(context.Background(),
		`SELECT i.id,p.title,p.slug,p.price_cents,i.qty,i.later,i.fav,i.opts::text
		 FROM cart_items i JOIN products p ON p.id=i.product_id WHERE i.cart_id=$1 ORDER BY i.id`, cid)
	var items []map[string]any
	total := 0
	for rows.Next() {
		var id int64
		var title, slug, opts string
		var pc, qty int
		var later, fav bool
		_ = rows.Scan(&id, &title, &slug, &pc, &qty, &later, &fav, &opts)
		if !later {
			total += pc * qty
		}
		items = append(items, map[string]any{"ID": id, "Title": title, "Slug": slug, "Price": fmt.Sprintf("%.2f", float64(pc)/100), "Qty": qty, "Later": later, "Fav": fav, "Opts": opts})
	}
	rows.Close()
	var coupon string
	_ = pool.QueryRow(context.Background(), `SELECT coalesce(coupon,'') FROM carts WHERE id=$1`, cid).Scan(&coupon)
	disc, coupon := s.applyCoupon(coupon, total)
	p := s.base(r, "Cart")
	s.decorate(&p, "shop")
	p.Data = map[string]any{"Items": items, "Total": fmt.Sprintf("%.2f", float64(total-disc)/100), "Sub": fmt.Sprintf("%.2f", float64(total)/100), "Discount": fmt.Sprintf("%.2f", float64(disc)/100), "Coupon": coupon}
	s.render(w, "cart.html", p)
}

func (s *Server) cartCheckout(w http.ResponseWriter, r *http.Request, cid string) {
	pool, _ := s.db()
	rows, _ := pool.Query(context.Background(),
		`SELECT p.id,p.title,p.price_cents,i.qty FROM cart_items i JOIN products p ON p.id=i.product_id WHERE i.cart_id=$1 AND i.later=false`, cid)
	type line struct {
		id, title  string
		pid        int64
		price, qty int
	}
	var lines []line
	total := 0
	for rows.Next() {
		var l line
		_ = rows.Scan(&l.pid, &l.title, &l.price, &l.qty)
		total += l.price * l.qty
		lines = append(lines, l)
	}
	rows.Close()
	if total == 0 {
		http.Redirect(w, r, "/cart", http.StatusSeeOther)
		return
	}
	for _, l := range lines {
		if !s.stockOK(l.pid, l.qty) {
			http.Error(w, "out of stock: "+l.title, 409)
			return
		}
	}
	var coupon string
	_ = pool.QueryRow(context.Background(), `SELECT coalesce(coupon,'') FROM carts WHERE id=$1`, cid).Scan(&coupon)
	if c := r.FormValue("coupon"); c != "" {
		coupon = c
	}
	disc, coupon := s.applyCoupon(coupon, total)
	via := val(r, "pay_via", "invoice")
	st, zip := r.FormValue("state"), r.FormValue("zip")
	taxc := tax.Cents(total-disc, "US", st, zip)
	email := r.FormValue("email")
	var uid *int64
	if u := s.user(r); u != nil {
		uid = &u.ID
		email = u.Email
	}
	var oid int64
	_ = pool.QueryRow(context.Background(),
		`INSERT INTO orders(user_id,email,total_cents,tax_cents,status,dest_state,dest_zip,pay_via,kind,coupon,discount_cents)
		 VALUES($1,$2,$3,$4,'pending',$5,$6,$7,'cart',$8,$9) RETURNING id`,
		uid, email, total-disc+taxc, taxc, st, zip, via, coupon, disc).Scan(&oid)
	for _, l := range lines {
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO order_items(order_id,product_id,title,qty,price_cents) VALUES($1,$2,$3,$4,$5)`,
			oid, l.pid, l.title, l.qty, l.price)
	}
	origin := strings.TrimRight(s.cfg.URL, "/")
	intent := pay.Intent{OrderID: strconv.FormatInt(oid, 10), AmountCents: total - disc + taxc, Currency: "USD", Title: "Cart", Email: email,
		SuccessURL: origin + "/pay/return?order=" + strconv.FormatInt(oid, 10), CancelURL: origin + "/cart"}
	switch via {
	case "stripe":
		sess, err := pay.StripeSession(s.payCfg(), intent)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		http.Redirect(w, r, sess.Redirect, http.StatusSeeOther)
	case "paypal":
		sess, err := pay.PayPalSession(s.payCfg(), intent)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		http.Redirect(w, r, sess.Redirect, http.StatusSeeOther)
	case "crypto":
		_, _ = pool.Exec(context.Background(), `INSERT INTO crypto_payments(order_id) VALUES($1)`, oid)
		p := s.base(r, "Pay with crypto")
		s.decorate(&p, "shop")
		p.Data = map[string]any{"Order": oid, "Total": fmt.Sprintf("%.2f", float64(total+taxc)/100), "Coins": s.payCfg().Coins, "Hint": "Prepaid. Include order #" + strconv.FormatInt(oid, 10)}
		s.render(w, "pay-crypto.html", p)
	default:
		s.fulfill(oid)
		http.Redirect(w, r, "/pay/return?order="+strconv.FormatInt(oid, 10), http.StatusSeeOther)
	}
}
