package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PacificDailyTimes/pdt-news/internal/mailer"
	"github.com/PacificDailyTimes/pdt-news/internal/pay"
	"github.com/PacificDailyTimes/pdt-news/internal/tax"
)

func (s *Server) checkout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/shop", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	pool, _ := s.db()
	pid, _ := strconv.ParseInt(r.FormValue("product_id"), 10, 64)
	var title, kind string
	var price int
	var n *int
	var unit *string
	err := pool.QueryRow(context.Background(),
		`SELECT title,price_cents,kind,interval_n,interval_unit FROM products WHERE id=$1`, pid).
		Scan(&title, &price, &kind, &n, &unit)
	if err != nil {
		http.Error(w, "product", 404)
		return
	}
	via := val(r, "pay_via", "invoice")
	interval := ""
	in := 1
	if unit != nil {
		interval = *unit
	}
	if n != nil {
		in = *n
	}
	renew := pay.Recurring(via, kind, r.FormValue("renew") == "1")
	st, zip := r.FormValue("state"), r.FormValue("zip")
	taxc := tax.Cents(price, "US", st, zip)
	email := r.FormValue("email")
	var uid *int64
	if u := s.user(r); u != nil {
		uid = &u.ID
		email = u.Email
	}
	end := pay.PeriodEnd(in, interval)
	var oid int64
	_ = pool.QueryRow(context.Background(),
		`INSERT INTO orders(user_id,email,total_cents,tax_cents,status,dest_state,dest_zip,pay_via,renew,period_end,kind)
		 VALUES($1,$2,$3,$4,'pending',$5,$6,$7,$8,$9,$10) RETURNING id`,
		uid, email, price+taxc, taxc, st, zip, via, renew, end, kind).Scan(&oid)
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO order_items(order_id,product_id,title,qty,price_cents) VALUES($1,$2,$3,1,$4)`,
		oid, pid, title, price)

	origin := strings.TrimRight(s.cfg.URL, "/")
	intent := pay.Intent{
		OrderID: strconv.FormatInt(oid, 10), AmountCents: price + taxc, Currency: "USD",
		Title: title, Email: email, IntervalN: in, Interval: interval, Renew: renew,
		SuccessURL: origin + "/pay/return?order=" + strconv.FormatInt(oid, 10),
		CancelURL:  origin + "/shop",
	}

	switch via {
	case "stripe":
		sess, err := pay.StripeSession(s.payCfg(), intent)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		_, _ = pool.Exec(context.Background(), `UPDATE orders SET provider_ref=$1 WHERE id=$2`, sess.Ref, oid)
		http.Redirect(w, r, sess.Redirect, http.StatusSeeOther)
	case "paypal":
		sess, err := pay.PayPalSession(s.payCfg(), intent)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		_, _ = pool.Exec(context.Background(), `UPDATE orders SET provider_ref=$1 WHERE id=$2`, sess.Ref, oid)
		http.Redirect(w, r, sess.Redirect, http.StatusSeeOther)
	case "crypto":
		_, _ = pool.Exec(context.Background(), `INSERT INTO crypto_payments(order_id) VALUES($1)`, oid)
		p := s.base(r, "Pay with crypto")
		p.Data = map[string]any{
			"Order": oid, "Total": fmt.Sprintf("%.2f", float64(price+taxc)/100),
			"Coins": s.payCfg().Coins, "Hint": "Prepaid. This membership/run does not renew. Include order #" + strconv.FormatInt(oid, 10) + " in the memo.",
		}
		s.render(w, "pay-crypto.html", p)
	default: // invoice
		s.fulfill(oid)
		http.Redirect(w, r, "/pay/return?order="+strconv.FormatInt(oid, 10), http.StatusSeeOther)
	}
}

func (s *Server) payReturn(w http.ResponseWriter, r *http.Request) {
	oid, _ := strconv.ParseInt(r.URL.Query().Get("order"), 10, 64)
	if token := r.URL.Query().Get("token"); token != "" {
		_ = pay.CapturePayPal(s.payCfg(), token)
		pool, _ := s.db()
		var id int64
		_ = pool.QueryRow(context.Background(), `SELECT id FROM orders WHERE provider_ref=$1`, token).Scan(&id)
		if id > 0 {
			oid = id
			s.fulfill(oid)
		}
	}
	if oid > 0 {
		pool, _ := s.db()
		var st, via string
		_ = pool.QueryRow(context.Background(), `SELECT status,pay_via FROM orders WHERE id=$1`, oid).Scan(&st, &via)
		if st == "pending" && (via == "stripe" || via == "paypal") {
			s.fulfill(oid) // return URL is enough when webhooks lag; webhook is idempotent
		}
	}
	p := s.base(r, "Receipt")
	p.Flash = "Order recorded. Stripe/PayPal subscriptions renew until you cancel with the processor. Crypto never renews."
	s.render(w, "receipt.html", p)
}

func (s *Server) payStripeWH(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var ev struct {
		Type string `json:"type"`
		Data struct {
			Object map[string]any `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &ev) != nil {
		http.Error(w, "json", 400)
		return
	}
	if ev.Type == "checkout.session.completed" || ev.Type == "invoice.paid" {
		ref := strAny(ev.Data.Object["id"])
		client := strAny(ev.Data.Object["client_reference_id"])
		pool, _ := s.db()
		var oid int64
		if client != "" {
			oid, _ = strconv.ParseInt(client, 10, 64)
		}
		if oid == 0 && ref != "" {
			_ = pool.QueryRow(context.Background(), `SELECT id FROM orders WHERE provider_ref=$1`, ref).Scan(&oid)
		}
		if oid > 0 {
			s.fulfill(oid)
		}
	}
	w.WriteHeader(200)
}

func (s *Server) payPaypalWH(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var ev map[string]any
	_ = json.Unmarshal(b, &ev)
	et := strAny(ev["event_type"])
	res, _ := ev["resource"].(map[string]any)
	if et == "CHECKOUT.ORDER.APPROVED" || et == "PAYMENT.CAPTURE.COMPLETED" || et == "BILLING.SUBSCRIPTION.ACTIVATED" {
		id := strAny(res["id"])
		custom := strAny(res["custom_id"])
		if custom == "" {
			custom = strAny(res["reference_id"])
		}
		pool, _ := s.db()
		var oid int64
		if custom != "" {
			oid, _ = strconv.ParseInt(custom, 10, 64)
		}
		if oid == 0 && id != "" {
			_ = pool.QueryRow(context.Background(), `SELECT id FROM orders WHERE provider_ref=$1`, id).Scan(&oid)
		}
		if oid > 0 {
			s.fulfill(oid)
		}
	}
	w.WriteHeader(200)
}

func (s *Server) payCryptoNote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/shop", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	oid, _ := strconv.ParseInt(r.FormValue("order"), 10, 64)
	pool, _ := s.db()
	_, _ = pool.Exec(context.Background(),
		`UPDATE crypto_payments SET status='awaiting', note=$1 WHERE order_id=$2`,
		r.FormValue("txid"), oid)
	p := s.base(r, "Crypto")
	p.Flash = "Marked as sent. An admin will confirm. This is prepaid — it will not renew."
	s.render(w, "receipt.html", p)
}

func (s *Server) dashPay(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "admin")
	if u == nil {
		return
	}
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		if id := r.FormValue("confirm"); id != "" {
			oid, _ := strconv.ParseInt(id, 10, 64)
			s.fulfill(oid)
			_, _ = pool.Exec(context.Background(), `UPDATE crypto_payments SET status='confirmed' WHERE order_id=$1`, oid)
		}
		http.Redirect(w, r, "/dash/pay", http.StatusSeeOther)
		return
	}
	p := s.base(r, "Payments")
	rows, _ := pool.Query(context.Background(),
		`SELECT o.id, coalesce(o.email,''), o.total_cents, coalesce(o.pay_via,''), o.status, o.renew, coalesce(o.kind,'')
		 FROM orders o ORDER BY o.id DESC LIMIT 80`)
	var list []map[string]any
	for rows.Next() {
		var id int64
		var email, via, st, kind string
		var total int
		var renew bool
		_ = rows.Scan(&id, &email, &total, &via, &st, &renew, &kind)
		list = append(list, map[string]any{
			"ID": id, "Email": email, "Total": fmt.Sprintf("%.2f", float64(total)/100),
			"Via": via, "Status": st, "Renew": renew, "Kind": kind,
		})
	}
	rows.Close()
	p.Data = list
	s.render(w, "dash-pay.html", p)
}

func (s *Server) fulfill(oid int64) {
	pool, err := s.db()
	if err != nil || oid < 1 {
		return
	}
	var status, via, kind, email string
	var uid *int64
	var pid *int64
	var title string
	var price, taxc int
	var renew bool
	var n *int
	var unit *string
	err = pool.QueryRow(context.Background(),
		`SELECT o.status,o.pay_via,coalesce(o.kind,''),o.email,o.user_id,o.total_cents,o.tax_cents,o.renew,
		        oi.product_id,oi.title,p.interval_n,p.interval_unit
		 FROM orders o
		 JOIN order_items oi ON oi.order_id=o.id
		 LEFT JOIN products p ON p.id=oi.product_id
		 WHERE o.id=$1 LIMIT 1`, oid).
		Scan(&status, &via, &kind, &email, &uid, &price, &taxc, &renew, &pid, &title, &n, &unit)
	if err != nil {
		return
	}
	if status == "paid" {
		return
	}
	end := pay.PeriodEnd(1, "month")
	if n != nil && unit != nil {
		end = pay.PeriodEnd(*n, *unit)
	}
	_, _ = pool.Exec(context.Background(),
		`UPDATE orders SET status='paid', period_end=$1 WHERE id=$2`, end, oid)
	items, _ := pool.Query(context.Background(),
		`SELECT product_id, qty FROM order_items WHERE order_id=$1 AND product_id IS NOT NULL`, oid)
	for items.Next() {
		var pid int64
		var qty int
		if items.Scan(&pid, &qty) != nil {
			continue
		}
		_, _ = pool.Exec(context.Background(),
			`UPDATE products SET stock = stock - $1 WHERE id=$2 AND stock IS NOT NULL AND stock >= $1`, qty, pid)
		if uid != nil {
			_, _ = pool.Exec(context.Background(),
				`INSERT INTO entitlements(user_id,product_id,source) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
				*uid, pid, via)
		}
	}
	items.Close()
	var coupon string
	_ = pool.QueryRow(context.Background(), `SELECT coalesce(coupon,'') FROM orders WHERE id=$1`, oid).Scan(&coupon)
	if coupon != "" {
		s.bumpCoupon(coupon)
	}
	if uid != nil && pid != nil {
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO entitlements(user_id,product_id,source) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
			*uid, *pid, via)
		if kind == "membership" {
			_, _ = pool.Exec(context.Background(), `UPDATE users SET member_until=$1 WHERE id=$2`, end, *uid)
		}
		if kind == "subscription" || kind == "membership" {
			intervalN, intervalU := 1, "month"
			if n != nil {
				intervalN = *n
			}
			if unit != nil {
				intervalU = *unit
			}
			var exp any
			if !renew {
				exp = end.Format("2006-01-02")
			}
			_, _ = pool.Exec(context.Background(),
				`INSERT INTO subscriptions(user_id,product_id,interval_n,interval_unit,start_on,next_on,pay_via,renew,expires_on)
				 VALUES($1,$2,$3,$4,CURRENT_DATE,CURRENT_DATE,$5,$6,$7)`,
				*uid, *pid, intervalN, intervalU, via, renew, exp)
		}
	}
	num := fmt.Sprintf("INV-%d", oid)
	pdf := simplePDF(num, title, price-taxc, taxc)
	pdfPath := filepath.Join(s.root, "var", "invoices")
	_ = os.MkdirAll(pdfPath, 0755)
	fn := filepath.Join(pdfPath, num+".pdf")
	_ = os.WriteFile(fn, pdf, 0644)
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO invoices(order_id,number,pdf_path) VALUES($1,$2,$3) ON CONFLICT (number) DO NOTHING`, oid, num, fn)
	if email != "" {
		_ = mailer.SendPDF(s.cfg, email, "Invoice "+num,
			"Thank you.\n\nTotal: $"+fmt.Sprintf("%.2f", float64(price)/100)+"\nPay via "+via+"\nRenews: "+strconv.FormatBool(renew)+"\n",
			pdf, num+".pdf")
	}
	_ = time.Now
}

func strAny(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
