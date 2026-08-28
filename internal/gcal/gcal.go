package gcal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PacificDailyTimes/pdt-news/internal/caldav"
	"github.com/PacificDailyTimes/pdt-news/internal/config"
)

type Client struct {
	Access, Refresh, CalID string
	Cfg                    *config.Config
}

func (c *Client) cal() string {
	if c.CalID == "" {
		return "primary"
	}
	return url.PathEscape(c.CalID)
}

func (c *Client) List(from, to time.Time) []caldav.Event {
	c.ensure()
	q := url.Values{
		"timeMin":      {from.UTC().Format(time.RFC3339)},
		"timeMax":      {to.UTC().Format(time.RFC3339)},
		"singleEvents": {"true"},
		"orderBy":      {"startTime"},
		"maxResults":   {"250"},
	}
	raw, err := c.do(http.MethodGet, "https://www.googleapis.com/calendar/v3/calendars/"+c.cal()+"/events?"+q.Encode(), nil)
	if err != nil {
		return nil
	}
	var resp struct {
		Items []gEvent `json:"items"`
	}
	_ = json.Unmarshal(raw, &resp)
	var out []caldav.Event
	for _, it := range resp.Items {
		e := it.event()
		if !e.Start.IsZero() {
			out = append(out, e)
		}
	}
	return out
}

func (c *Client) Put(ev caldav.Event) (caldav.Event, error) {
	c.ensure()
	body, _ := json.Marshal(gEvent{
		Summary: ev.Summary,
		Start:   gTime{DateTime: ev.Start.Format(time.RFC3339)},
		End:     gTime{DateTime: ev.End.Format(time.RFC3339)},
		ICalUID: ev.UID,
		Ext:     gExt{Private: map[string]string{"pdt_uid": ev.UID}},
	})
	path := "https://www.googleapis.com/calendar/v3/calendars/" + c.cal() + "/events"
	method := http.MethodPost
	if ev.Href != "" {
		path += "/" + url.PathEscape(ev.Href)
		method = http.MethodPatch
	}
	raw, err := c.do(method, path, body)
	if err != nil {
		return ev, err
	}
	var it gEvent
	_ = json.Unmarshal(raw, &it)
	return it.event(), nil
}

func (c *Client) Delete(id string) error {
	c.ensure()
	_, err := c.do(http.MethodDelete, "https://www.googleapis.com/calendar/v3/calendars/"+c.cal()+"/events/"+url.PathEscape(id), nil)
	return err
}

func (c *Client) ensure() {
	if c.Access != "" || c.Cfg == nil || c.Refresh == "" {
		return
	}
	tok, err := post("https://oauth2.googleapis.com/token", url.Values{
		"client_id":     {c.Cfg.GoogleID},
		"client_secret": {c.Cfg.GoogleSecret},
		"refresh_token": {c.Refresh},
		"grant_type":    {"refresh_token"},
	})
	if err == nil {
		if a, _ := tok["access_token"].(string); a != "" {
			c.Access = a
		}
	}
}

func (c *Client) do(method, dest string, body []byte) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, dest, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Access)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode == 401 && c.Refresh != "" && c.Cfg != nil {
		c.Access = ""
		c.ensure()
		if c.Access != "" {
			return c.do(method, dest, body)
		}
	}
	if resp.StatusCode >= 300 {
		return b, fmt.Errorf("gcal %d: %s", resp.StatusCode, b)
	}
	return b, nil
}

type gTime struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
}
type gExt struct {
	Private map[string]string `json:"private,omitempty"`
}
type gEvent struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
	ICalUID string `json:"iCalUID"`
	Start   gTime  `json:"start"`
	End     gTime  `json:"end"`
	Ext     gExt   `json:"extendedProperties"`
}

func (g gEvent) event() caldav.Event {
	uid := g.ICalUID
	if g.Ext.Private != nil && g.Ext.Private["pdt_uid"] != "" {
		uid = g.Ext.Private["pdt_uid"]
	}
	return caldav.Event{
		UID: uid, Href: g.ID, Summary: g.Summary,
		Start: parseGTime(g.Start), End: parseGTime(g.End),
	}
}

func parseGTime(t gTime) time.Time {
	if t.DateTime != "" {
		if x, err := time.Parse(time.RFC3339, t.DateTime); err == nil {
			return x
		}
	}
	if t.Date != "" {
		if x, err := time.Parse("2006-01-02", t.Date); err == nil {
			return x
		}
	}
	return time.Time{}
}

func post(dest string, v url.Values) (map[string]any, error) {
	req, _ := http.NewRequest(http.MethodPost, dest, strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m, nil
}
