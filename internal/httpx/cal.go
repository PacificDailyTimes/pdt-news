package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PacificDailyTimes/pdt-news/internal/caldav"
	"github.com/PacificDailyTimes/pdt-news/internal/pay"
)

func (s *Server) dashCal(w http.ResponseWriter, r *http.Request) {
	if !s.flags().Appointments.On {
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
		if r.FormValue("act") == "sync" {
			s.dashCalSync(w, r)
			return
		}
		if r.FormValue("act") == "event" {
			cid, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
			start, end := parseLocal(r.FormValue("start")), parseLocal(r.FormValue("end"))
			if end.IsZero() {
				end = start.Add(30 * time.Minute)
			}
			ev, _ := s.loadConn(cid).Put(caldav.Event{
				UID: r.FormValue("uid"), Href: r.FormValue("href"),
				Summary: r.FormValue("title"), Start: start, End: end,
			})
			_ = ev
			http.Redirect(w, r, "/dash/cal?id="+r.FormValue("id"), http.StatusSeeOther)
			return
		}
		if r.FormValue("act") == "delete_event" {
			cid, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
			_ = s.loadConn(cid).Delete(r.FormValue("href"))
			http.Redirect(w, r, "/dash/cal?id="+r.FormValue("id"), http.StatusSeeOther)
			return
		}
		var sid int64
		_ = pool.QueryRow(context.Background(), `SELECT id FROM sites WHERE is_main=true`).Scan(&sid)
		slot, _ := strconv.Atoi(val(r, "slot_min", "30"))
		min, _ := strconv.Atoi(r.FormValue("min_tier"))
		win := r.FormValue("windows")
		if win == "" {
			win = `{"mon":[["09:00","17:00"]],"tue":[["09:00","17:00"]],"wed":[["09:00","17:00"]],"thu":[["09:00","17:00"]],"fri":[["09:00","17:00"]],"sat":[],"sun":[]}`
		}
		kind := val(r, "kind", "caldav")
		davURL := r.FormValue("caldav_url")
		davUser := r.FormValue("caldav_user")
		davPass := r.FormValue("caldav_pass")
		if kind == "apple" {
			acc := caldav.Acc{URL: val(r, "caldav_url", "https://caldav.icloud.com/"), User: davUser, Pass: davPass}
			if d, err := acc.Discover(); err == nil && d != "" {
				davURL = d
			}
		}
		if kind == "caldav" && davURL != "" && !strings.Contains(davURL, "/calendars/") {
			acc := caldav.Acc{URL: davURL, User: davUser, Pass: davPass}
			if d, err := acc.Discover(); err == nil && d != "" {
				davURL = d
			}
		}
		_, _ = pool.Exec(context.Background(),
			`INSERT INTO calendars(site_id,name,slug,tz,slot_min,windows,ical_url,caldav_url,caldav_user,caldav_pass,min_tier,word,product_id,kind)
			 VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12,$13,$14)`,
			sid, r.FormValue("name"), slugify(val(r, "slug", r.FormValue("name"))),
			val(r, "tz", "America/Detroit"), slot, win, nullStr(r.FormValue("ical_url")),
			nullStr(davURL), nullStr(davUser), nullStr(davPass),
			min, val(r, "word", s.bookWord()),
			nullInt(r.FormValue("product_id")), kind)
		if r.FormValue("book_word") != "" {
			_, _ = pool.Exec(context.Background(), `UPDATE sites SET book_word=$1 WHERE is_main=true`, r.FormValue("book_word"))
		}
		http.Redirect(w, r, "/dash/cal", http.StatusSeeOther)
		return
	}
	p := s.base(r, s.bookWord())
	s.decorate(&p, "")
	rows, _ := pool.Query(context.Background(), `SELECT id,name,slug,slot_min,word,coalesce(kind,'caldav') FROM calendars ORDER BY id`)
	var list []map[string]any
	for rows.Next() {
		var id int64
		var n, sl, word, kind string
		var slot int
		_ = rows.Scan(&id, &n, &sl, &slot, &word, &kind)
		list = append(list, map[string]any{"ID": id, "Name": n, "Slug": sl, "Slot": slot, "Word": word, "Kind": kind})
	}
	rows.Close()
	data := map[string]any{"Cals": list, "GoogleOn": s.cfg.GoogleID != ""}
	if qid := r.URL.Query().Get("id"); qid != "" {
		cid, _ := strconv.ParseInt(qid, 10, 64)
		s.remoteSync(cid)
		from := time.Now().Add(-12 * time.Hour)
		to := time.Now().Add(14 * 24 * time.Hour)
		var evs []map[string]any
		for _, e := range s.loadConn(cid).List(from, to) {
			evs = append(evs, map[string]any{
				"Title": e.Summary, "Start": e.Start.Local().Format("Mon 2 Jan 15:04"),
				"End": e.End.Local().Format("15:04"), "Href": e.Href, "UID": e.UID,
			})
		}
		data["Open"] = qid
		data["Events"] = evs
	}
	p.Data = data
	s.render(w, "dash-cal.html", p)
}

func (s *Server) bookWord() string {
	w := s.setting("book_word")
	if w != "" {
		return w
	}
	pool, err := s.db()
	if err != nil {
		return "appointment"
	}
	var v string
	_ = pool.QueryRow(context.Background(), `SELECT coalesce(book_word,'appointment') FROM sites WHERE is_main=true`).Scan(&v)
	if v == "" {
		return "appointment"
	}
	return v
}

func (s *Server) pubCal(w http.ResponseWriter, r *http.Request, slug string) {
	if !s.flags().Appointments.On {
		http.NotFound(w, r)
		return
	}
	pool, _ := s.db()
	var id int64
	var name, tz, win, word string
	var slot, min int
	var ical *string
	var pid *int64
	err := pool.QueryRow(context.Background(),
		`SELECT id,name,tz,slot_min,windows::text,ical_url,min_tier,word,product_id FROM calendars WHERE slug=$1`, slug).
		Scan(&id, &name, &tz, &slot, &win, &ical, &min, &word, &pid)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.gate(w, r, min, name) {
		return
	}
	s.remoteSync(id)
	conn := s.loadConn(id)
	busy := s.busyTimes(id, ical, conn)
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		u := s.user(r)
		start, err := time.ParseInLocation("2006-01-02 15:04", r.FormValue("day")+" "+r.FormValue("slot"), time.Local)
		if err != nil {
			http.Error(w, "bad slot", 400)
			return
		}
		end := start.Add(time.Duration(slot) * time.Minute)
		for _, b := range busy {
			if start.Before(b[1]) && end.After(b[0]) {
				http.Error(w, "that slot is taken", 409)
				return
			}
		}
		email := r.FormValue("email")
		var uid *int64
		if u != nil {
			uid = &u.ID
			email = u.Email
		}
		var bid int64
		_ = pool.QueryRow(context.Background(),
			`INSERT INTO bookings(calendar_id,user_id,email,starts,ends,status,note) VALUES($1,$2,$3,$4,$5,'held',$6) RETURNING id`,
			id, uid, email, start, end, r.FormValue("note")).Scan(&bid)
		if pid != nil && r.FormValue("pay_via") != "" && r.FormValue("pay_via") != "none" {
			var title string
			var price int
			_ = pool.QueryRow(context.Background(), `SELECT title,price_cents FROM products WHERE id=$1`, *pid).Scan(&title, &price)
			via := r.FormValue("pay_via")
			renew := false
			var oid int64
			_ = pool.QueryRow(context.Background(),
				`INSERT INTO orders(user_id,email,total_cents,status,pay_via,renew,kind) VALUES($1,$2,$3,'pending',$4,$5,'booking') RETURNING id`,
				uid, email, price, via, renew).Scan(&oid)
			origin := strings.TrimRight(s.cfg.URL, "/")
			intent := pay.Intent{OrderID: strconv.FormatInt(oid, 10), AmountCents: price, Currency: "USD", Title: title, Email: email,
				SuccessURL: origin + "/pay/return?order=" + strconv.FormatInt(oid, 10), CancelURL: origin + "/book/" + slug}
			switch via {
			case "stripe":
				sess, err := pay.StripeSession(s.payCfg(), intent)
				if err == nil {
					http.Redirect(w, r, sess.Redirect, http.StatusSeeOther)
					return
				}
			case "paypal":
				sess, err := pay.PayPalSession(s.payCfg(), intent)
				if err == nil {
					http.Redirect(w, r, sess.Redirect, http.StatusSeeOther)
					return
				}
			}
		}
		_, _ = pool.Exec(context.Background(), `UPDATE bookings SET status='booked' WHERE id=$1`, bid)
		if conn.kind != "" || conn.dav.URL != "" || conn.g != nil {
			ev, err := conn.Put(caldav.Event{
				UID:     fmt.Sprintf("pdt-%d@pdt", bid),
				Start:   start,
				End:     end,
				Summary: name,
			})
			if err == nil {
				_, _ = pool.Exec(context.Background(),
					`UPDATE bookings SET caldav_uid=$1, caldav_href=$2, caldav_etag=$3 WHERE id=$4`,
					ev.UID, ev.Href, ev.ETag, bid)
			}
		}
		p := s.base(r, word)
		s.decorate(&p, "page")
		p.Flash = "Booked " + start.Format("Mon 2 Jan 15:04") + "."
		s.render(w, "cal.html", p)
		return
	}
	day := r.URL.Query().Get("day")
	if day == "" {
		day = time.Now().Format("2006-01-02")
	}
	slots := s.openSlots(win, slot, day, busy)
	p := s.base(r, name)
	s.decorate(&p, "page")
	p.Data = map[string]any{"Name": name, "Slug": slug, "Word": word, "Day": day, "Slots": slots, "SlotMin": slot, "NeedPay": pid != nil}
	s.render(w, "cal.html", p)
}

func (s *Server) ics(w http.ResponseWriter, r *http.Request, slug string) {
	pool, _ := s.db()
	var id int64
	var name string
	if err := pool.QueryRow(context.Background(), `SELECT id,name FROM calendars WHERE slug=$1`, slug).Scan(&id, &name); err != nil {
		http.NotFound(w, r)
		return
	}
	rows, _ := pool.Query(context.Background(),
		`SELECT starts,ends,coalesce(note,'') FROM bookings WHERE calendar_id=$1 AND status IN ('booked','held') ORDER BY starts`, id)
	w.Header().Set("Content-Type", "text/calendar")
	fmt.Fprint(w, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//pdt-news//EN\r\n")
	for rows.Next() {
		var a, b time.Time
		var note string
		_ = rows.Scan(&a, &b, &note)
		fmt.Fprintf(w, "BEGIN:VEVENT\r\nDTSTART:%s\r\nDTEND:%s\r\nSUMMARY:%s\r\nDESCRIPTION:%s\r\nEND:VEVENT\r\n",
			a.UTC().Format("20060102T150405Z"), b.UTC().Format("20060102T150405Z"), icsEsc(name), icsEsc(note))
	}
	rows.Close()
	fmt.Fprint(w, "END:VCALENDAR\r\n")
}

func (s *Server) dashCalSync(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "admin", "author")
	if u == nil {
		return
	}
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	s.remoteSync(id)
	http.Redirect(w, r, "/dash/cal?id="+r.FormValue("id"), http.StatusSeeOther)
}

func (s *Server) busyTimes(calID int64, ical *string, c calConn) [][2]time.Time {
	var out [][2]time.Time
	pool, _ := s.db()
	rows, _ := pool.Query(context.Background(),
		`SELECT starts,ends FROM bookings WHERE calendar_id=$1 AND status IN ('booked','held')`, calID)
	for rows.Next() {
		var a, b time.Time
		_ = rows.Scan(&a, &b)
		out = append(out, [2]time.Time{a, b})
	}
	rows.Close()
	if ical != nil && *ical != "" {
		out = append(out, fetchICS(*ical)...)
	}
	from := time.Now().Add(-24 * time.Hour)
	to := time.Now().Add(60 * 24 * time.Hour)
	for _, e := range c.List(from, to) {
		out = append(out, [2]time.Time{e.Start, e.End})
	}
	return out
}

func fetchICS(u string) [][2]time.Time {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return caldav.ParseICS(string(b))
}

func (s *Server) openSlots(winJSON string, slot int, day string, busy [][2]time.Time) []string {
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		return nil
	}
	var win map[string][][]string
	_ = json.Unmarshal([]byte(winJSON), &win)
	keys := []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
	key := keys[int(d.Weekday())]
	ranges := win[key]
	var out []string
	for _, rg := range ranges {
		if len(rg) < 2 {
			continue
		}
		a, _ := time.Parse("15:04", rg[0])
		b, _ := time.Parse("15:04", rg[1])
		cur := time.Date(d.Year(), d.Month(), d.Day(), a.Hour(), a.Minute(), 0, 0, time.Local)
		end := time.Date(d.Year(), d.Month(), d.Day(), b.Hour(), b.Minute(), 0, 0, time.Local)
		for cur.Add(time.Duration(slot)*time.Minute).Before(end) || cur.Add(time.Duration(slot)*time.Minute).Equal(end) {
			slotEnd := cur.Add(time.Duration(slot) * time.Minute)
			taken := false
			for _, x := range busy {
				if cur.Before(x[1]) && slotEnd.After(x[0]) {
					taken = true
					break
				}
			}
			if !taken && cur.After(time.Now()) {
				out = append(out, cur.Format("15:04"))
			}
			cur = slotEnd
		}
	}
	return out
}

func icsEsc(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, ",", `\,`)
	s = strings.ReplaceAll(s, ";", `\;`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
