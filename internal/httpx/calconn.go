package httpx

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PacificDailyTimes/pdt-news/internal/caldav"
	"github.com/PacificDailyTimes/pdt-news/internal/gcal"
	"github.com/PacificDailyTimes/pdt-news/internal/oauth"
)

type calConn struct {
	kind string
	dav  caldav.Acc
	g    *gcal.Client
}

func (c calConn) List(from, to time.Time) []caldav.Event {
	if c.g != nil {
		return c.g.List(from, to)
	}
	return c.dav.List(from, to)
}

func (c calConn) Put(ev caldav.Event) (caldav.Event, error) {
	if c.g != nil {
		return c.g.Put(ev)
	}
	return c.dav.Put(ev)
}

func (c calConn) Delete(href string) error {
	if c.g != nil {
		return c.g.Delete(href)
	}
	return c.dav.Delete(href)
}

func (s *Server) loadConn(id int64) calConn {
	pool, err := s.db()
	if err != nil {
		return calConn{}
	}
	var kind, url, user, pass, ga, gr, gc string
	_ = pool.QueryRow(context.Background(),
		`SELECT coalesce(kind,'caldav'), coalesce(caldav_url,''), coalesce(caldav_user,''), coalesce(caldav_pass,''),
		        coalesce(google_access,''), coalesce(google_refresh,''), coalesce(google_cal,'primary')
		 FROM calendars WHERE id=$1`, id).
		Scan(&kind, &url, &user, &pass, &ga, &gr, &gc)
	c := calConn{kind: kind, dav: caldav.Acc{URL: url, User: user, Pass: pass}}
	if kind == "google" {
		c.g = &gcal.Client{Access: ga, Refresh: gr, CalID: gc, Cfg: s.cfg}
	}
	return c
}

func (s *Server) googleCalCallback(w http.ResponseWriter, r *http.Request, cb string) {
	u := s.user(r)
	if u == nil || (u.Role != "admin" && u.Role != "author") {
		http.Error(w, "sign in first", 401)
		return
	}
	access, refresh, err := oauth.GoogleExchange(s.cfg, r.URL.Query().Get("code"), cb)
	if err != nil || access == "" {
		http.Error(w, "google calendar oauth failed", 400)
		return
	}
	pool, _ := s.db()
	var sid int64
	_ = pool.QueryRow(context.Background(), `SELECT id FROM sites WHERE is_main=true`).Scan(&sid)
	slug := "google"
	_ = pool.QueryRow(context.Background(),
		`INSERT INTO calendars(site_id,name,slug,kind,google_access,google_refresh,google_cal,word)
		 VALUES($1,'Google','google','google',$2,$3,'primary',$4)
		 ON CONFLICT (site_id, slug) DO UPDATE SET google_access=$2, google_refresh=COALESCE(NULLIF($3,''), calendars.google_refresh)
		 RETURNING slug`,
		sid, access, refresh, s.bookWord()).Scan(&slug)
	http.Redirect(w, r, "/dash/cal", http.StatusSeeOther)
}

func (s *Server) remoteSync(calID int64) {
	c := s.loadConn(calID)
	from := time.Now().Add(-24 * time.Hour)
	to := time.Now().Add(90 * 24 * time.Hour)
	s.applyRemote(calID, c.List(from, to))
}

func (s *Server) applyRemote(calID int64, remote []caldav.Event) {
	pool, err := s.db()
	if err != nil {
		return
	}
	seen := map[string]caldav.Event{}
	for _, e := range remote {
		if e.UID == "" {
			continue
		}
		seen[e.UID] = e
		bid := pdtBookingID(e.UID)
		if bid < 1 {
			continue
		}
		_, _ = pool.Exec(context.Background(),
			`UPDATE bookings SET starts=$1, ends=$2, caldav_href=$3, caldav_etag=$4, status='booked'
			 WHERE id=$5 AND calendar_id=$6 AND status IN ('booked','held','cancelled')`,
			e.Start, e.End, e.Href, e.ETag, bid, calID)
	}
	rows, _ := pool.Query(context.Background(),
		`SELECT id, coalesce(caldav_uid,'') FROM bookings
		 WHERE calendar_id=$1 AND status IN ('booked','held') AND coalesce(caldav_uid,'') <> ''`, calID)
	defer rows.Close()
	for rows.Next() {
		var id int64
		var uid string
		if rows.Scan(&id, &uid) != nil {
			continue
		}
		if _, ok := seen[uid]; !ok {
			_, _ = pool.Exec(context.Background(), `UPDATE bookings SET status='cancelled' WHERE id=$1`, id)
		}
	}
}

func parseLocal(s string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

func pdtBookingID(uid string) int64 {
	u := strings.TrimSpace(uid)
	u = strings.TrimSuffix(u, "@pdt")
	if !strings.HasPrefix(u, "pdt-") {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimPrefix(u, "pdt-"), 10, 64)
	return n
}
