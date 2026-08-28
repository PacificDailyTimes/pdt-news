package agg

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type rss struct {
	Channel struct {
		Items []struct {
			Title string `xml:"title"`
			Link  string `xml:"link"`
			Desc  string `xml:"description"`
			Date  string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
}

func Loop(pool *pgxpool.Pool) {
	t := time.NewTicker(15 * time.Minute)
	defer t.Stop()
	for {
		Once(pool)
		<-t.C
	}
}

func Once(pool *pgxpool.Pool) {
	if pool == nil {
		return
	}
	rows, err := pool.Query(context.Background(),
		`SELECT id, site_id, source, coalesce(series_id,0) FROM aggregation WHERE status='active'`)
	if err != nil {
		return
	}
	defer rows.Close()
	type feed struct {
		ID, Site, Series int64
		Source           string
	}
	var feeds []feed
	for rows.Next() {
		var f feed
		_ = rows.Scan(&f.ID, &f.Site, &f.Source, &f.Series)
		feeds = append(feeds, f)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	for _, f := range feeds {
		resp, err := client.Get(f.Source)
		if err != nil {
			_, _ = pool.Exec(context.Background(), `UPDATE aggregation SET status='problematic' WHERE id=$1`, f.ID)
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var doc rss
		if xml.Unmarshal(b, &doc) != nil {
			continue
		}
		for _, it := range doc.Channel.Items {
			slug := it.Link
			if len(slug) > 80 {
				slug = slug[len(slug)-80:]
			}
			_, _ = pool.Exec(context.Background(),
				`INSERT INTO pieces(site_id,author_id,type,status,series_id,title,slug,content,excerpt,published_at)
				 SELECT $1, (SELECT id FROM users WHERE role='admin' LIMIT 1), 'post', 'live',
				        NULLIF($2,0), $3, $4, $5, $6, now()
				 WHERE NOT EXISTS (SELECT 1 FROM pieces WHERE site_id=$1 AND slug=$4)`,
				f.Site, f.Series, it.Title, slugify(slug), it.Desc, it.Desc)
		}
		_, _ = pool.Exec(context.Background(), `UPDATE aggregation SET last_fetch=now(), status='active' WHERE id=$1`, f.ID)
	}
}

func slugify(s string) string {
	out := make([]byte, 0, len(s))
	dash := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			out = append(out, c)
			dash = false
		} else if !dash {
			out = append(out, '-')
			dash = true
		}
	}
	return string(out)
}
