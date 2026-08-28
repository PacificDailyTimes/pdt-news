package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/PacificDailyTimes/pdt-news/internal/mailer"
)

func (s *Server) dashForms(w http.ResponseWriter, r *http.Request) {
	u := s.need(w, r, "admin", "author")
	if u == nil {
		return
	}
	pool, _ := s.db()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		var fields []map[string]any
		names := r.Form["fname"]
		labels := r.Form["flabel"]
		types := r.Form["ftype"]
		reqs := r.Form["freq"]
		for i, n := range names {
			if strings.TrimSpace(n) == "" {
				continue
			}
			lab, typ := n, "text"
			if i < len(labels) {
				lab = labels[i]
			}
			if i < len(types) {
				typ = types[i]
			}
			req := false
			if i < len(reqs) && reqs[i] == "1" {
				req = true
			}
			fields = append(fields, map[string]any{"name": n, "label": lab, "type": typ, "required": req})
		}
		b, _ := json.Marshal(fields)
		var sid int64
		_ = pool.QueryRow(context.Background(), `SELECT id FROM sites WHERE is_main=true`).Scan(&sid)
		min, _ := strconv.Atoi(r.FormValue("min_tier"))
		id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
		if id == 0 {
			_ = pool.QueryRow(context.Background(),
				`INSERT INTO forms(site_id,name,slug,fields,notify,thanks,min_tier) VALUES($1,$2,$3,$4::jsonb,$5,$6,$7) RETURNING id`,
				sid, r.FormValue("name"), slugify(val(r, "slug", r.FormValue("name"))), string(b),
				r.FormValue("notify"), val(r, "thanks", "Sent. We will reply."), min).Scan(&id)
		} else {
			_, _ = pool.Exec(context.Background(),
				`UPDATE forms SET name=$1,slug=$2,fields=$3::jsonb,notify=$4,thanks=$5,min_tier=$6 WHERE id=$7`,
				r.FormValue("name"), slugify(val(r, "slug", r.FormValue("name"))), string(b),
				r.FormValue("notify"), r.FormValue("thanks"), min, id)
		}
		http.Redirect(w, r, "/dash/forms", http.StatusSeeOther)
		return
	}
	p := s.base(r, "Contact forms")
	s.decorate(&p, "")
	rows, _ := pool.Query(context.Background(), `SELECT id,name,slug FROM forms ORDER BY id DESC`)
	var list []map[string]any
	for rows.Next() {
		var id int64
		var n, sl string
		_ = rows.Scan(&id, &n, &sl)
		list = append(list, map[string]any{"ID": id, "Name": n, "Slug": sl})
	}
	rows.Close()
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	form := map[string]any{"Fields": []map[string]any{
		{"Name": "name", "Label": "Name", "Type": "text", "Req": true},
		{"Name": "email", "Label": "Email", "Type": "email", "Req": true},
		{"Name": "message", "Label": "Message", "Type": "textarea", "Req": true},
	}}
	if id > 0 {
		var name, slug, fields, notify, thanks string
		var min int
		_ = pool.QueryRow(context.Background(),
			`SELECT name,slug,fields::text,coalesce(notify,''),thanks,min_tier FROM forms WHERE id=$1`, id).
			Scan(&name, &slug, &fields, &notify, &thanks, &min)
		var raw []map[string]any
		_ = json.Unmarshal([]byte(fields), &raw)
		var fs []map[string]any
		for _, f := range raw {
			fs = append(fs, map[string]any{"Name": f["name"], "Label": f["label"], "Type": f["type"], "Req": f["required"] == true})
		}
		form = map[string]any{"ID": id, "Name": name, "Slug": slug, "Notify": notify, "Thanks": thanks, "Min": min, "Fields": fs}
	}
	p.Data = map[string]any{"List": list, "Form": form}
	s.render(w, "dash-forms.html", p)
}

func (s *Server) pubForm(w http.ResponseWriter, r *http.Request, slug string) {
	pool, _ := s.db()
	var id int64
	var name, fields, thanks string
	var min int
	var notify *string
	err := pool.QueryRow(context.Background(),
		`SELECT id,name,fields::text,thanks,min_tier,notify FROM forms WHERE slug=$1`, slug).
		Scan(&id, &name, &fields, &thanks, &min, &notify)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.gate(w, r, min, name) {
		return
	}
	var fs []map[string]any
	_ = json.Unmarshal([]byte(fields), &fs)
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		payload := map[string]string{}
		for _, f := range fs {
			n, _ := f["name"].(string)
			payload[n] = r.FormValue(n)
		}
		b, _ := json.Marshal(payload)
		_, _ = pool.Exec(context.Background(), `INSERT INTO form_mail(form_id,payload) VALUES($1,$2::jsonb)`, id, string(b))
		if notify != nil && *notify != "" {
			body := ""
			for k, v := range payload {
				body += k + ": " + v + "\n"
			}
			_ = mailer.Send(s.cfg, *notify, "Form: "+name, body)
		}
		p := s.base(r, name)
		s.decorate(&p, "contact")
		p.Flash = thanks
		s.render(w, "form.html", p)
		return
	}
	p := s.base(r, name)
	s.decorate(&p, "contact")
	p.Data = map[string]any{"Name": name, "Slug": slug, "Fields": fs}
	s.render(w, "form.html", p)
}
