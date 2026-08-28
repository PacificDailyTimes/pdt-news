// Package caldav is a CalDAV *client* — the same role as iOS Calendar or DAVx⁵.
// Nextcloud / ownCloud / Fastmail / iCloud host the collection. We never do.
package caldav

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Acc struct {
	URL, User, Pass string
}

type Event struct {
	UID, Href, ETag, Summary string
	Start, End               time.Time
}

func (a Acc) ok() bool { return strings.TrimSpace(a.URL) != "" }

func (a Acc) col() string {
	u := strings.TrimSpace(a.URL)
	if u != "" && !strings.HasSuffix(u, "/") {
		u += "/"
	}
	return u
}

func (a Acc) Busy(from, to time.Time) [][2]time.Time {
	var out [][2]time.Time
	for _, e := range a.List(from, to) {
		out = append(out, [2]time.Time{e.Start, e.End})
	}
	return out
}

func (a Acc) List(from, to time.Time) []Event {
	if !a.ok() {
		return nil
	}
	if ev := a.getICS(); len(ev) > 0 {
		return filter(ev, from, to)
	}
	return filter(a.report(from, to), from, to)
}

func (a Acc) Put(ev Event) (Event, error) {
	if !a.ok() {
		return ev, nil
	}
	if ev.UID == "" {
		ev.UID = fmt.Sprintf("pdt-%d", time.Now().UnixNano())
	}
	href := ev.Href
	if href == "" {
		file := ev.UID
		if i := strings.IndexByte(file, '@'); i > 0 {
			file = file[:i]
		}
		href = a.col() + file + ".ics"
	}
	body := ics(ev)
	req, err := http.NewRequest(http.MethodPut, href, strings.NewReader(body))
	if err != nil {
		return ev, err
	}
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")
	if ev.ETag != "" {
		req.Header.Set("If-Match", ev.ETag)
	}
	a.auth(req)
	resp, err := httpClient().Do(req)
	if err != nil {
		return ev, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return ev, fmt.Errorf("caldav put %d: %s", resp.StatusCode, b)
	}
	ev.Href = href
	if t := resp.Header.Get("ETag"); t != "" {
		ev.ETag = t
	}
	return ev, nil
}

func (a Acc) Delete(href string) error {
	if href == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodDelete, href, nil)
	if err != nil {
		return err
	}
	a.auth(req)
	resp, err := httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != 404 {
		return fmt.Errorf("caldav delete %d", resp.StatusCode)
	}
	return nil
}

func (a Acc) auth(r *http.Request) {
	if a.User != "" {
		r.SetBasicAuth(a.User, a.Pass)
	}
}

func httpClient() *http.Client { return &http.Client{Timeout: 12 * time.Second} }

func (a Acc) getICS() []Event {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(a.URL, "/"), nil)
	if err != nil {
		return nil
	}
	a.auth(req)
	req.Header.Set("Accept", "text/calendar, text/plain, */*")
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if !strings.Contains(string(b), "BEGIN:VCALENDAR") {
		return nil
	}
	return ParseEvents(string(b), a.URL, "")
}

func (a Acc) report(from, to time.Time) []Event {
	q := `<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><d:getetag/><c:calendar-data/></d:prop>
  <c:filter>
    <c:comp-filter name="VCALENDAR">
      <c:comp-filter name="VEVENT">
        <c:time-range start="` + from.UTC().Format("20060102T150405Z") + `" end="` + to.UTC().Format("20060102T150405Z") + `"/>
      </c:comp-filter>
    </c:comp-filter>
  </c:filter>
</c:calendar-query>`
	req, err := http.NewRequest("REPORT", a.col(), strings.NewReader(q))
	if err != nil {
		return nil
	}
	a.auth(req)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", "1")
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return parseReport(string(b), a.col())
}

func parseReport(xml, base string) []Event {
	var out []Event
	chunks := splitCI(xml, "response")
	for _, ch := range chunks {
		href := abs(base, inner(ch, "href"))
		etag := strings.TrimSpace(inner(ch, "getetag"))
		data := inner(ch, "calendar-data")
		if data == "" {
			continue
		}
		for _, e := range ParseEvents(data, href, etag) {
			out = append(out, e)
		}
	}
	return out
}

func ParseEvents(s, href, etag string) []Event {
	var out []Event
	var e Event
	in := false
	for _, ln := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		switch {
		case ln == "BEGIN:VEVENT":
			in, e = true, Event{Href: href, ETag: etag}
		case !in:
			continue
		case strings.HasPrefix(ln, "UID"):
			e.UID = icsVal(ln)
		case strings.HasPrefix(ln, "SUMMARY"):
			e.Summary = icsVal(ln)
		case strings.HasPrefix(ln, "DTSTART"):
			e.Start = parseICSTime(ln)
		case strings.HasPrefix(ln, "DTEND"):
			e.End = parseICSTime(ln)
		case ln == "END:VEVENT":
			if e.End.IsZero() && !e.Start.IsZero() {
				e.End = e.Start.Add(30 * time.Minute)
			}
			if !e.Start.IsZero() {
				out = append(out, e)
			}
			in = false
		}
	}
	return out
}

func ParseICS(s string) [][2]time.Time {
	var out [][2]time.Time
	for _, e := range ParseEvents(s, "", "") {
		out = append(out, [2]time.Time{e.Start, e.End})
	}
	return out
}

func ics(ev Event) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//pdt-news//EN\r\nBEGIN:VEVENT\r\n" +
		"UID:" + ev.UID + "\r\n" +
		"DTSTAMP:" + time.Now().UTC().Format("20060102T150405Z") + "\r\n" +
		"DTSTART:" + ev.Start.UTC().Format("20060102T150405Z") + "\r\n" +
		"DTEND:" + ev.End.UTC().Format("20060102T150405Z") + "\r\n" +
		"SUMMARY:" + esc(ev.Summary) + "\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
}

func parseICSTime(ln string) time.Time {
	v := icsVal(ln)
	v = strings.TrimSuffix(v, "Z")
	for _, layout := range []string{"20060102T150405", "20060102"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

func icsVal(ln string) string {
	i := strings.LastIndex(ln, ":")
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(ln[i+1:])
}

func inner(s, name string) string {
	low := strings.ToLower(s)
	needle := strings.ToLower(name)
	i := strings.Index(low, needle)
	if i < 0 {
		return ""
	}
	gt := strings.Index(s[i:], ">")
	if gt < 0 {
		return ""
	}
	start := i + gt + 1
	j := strings.Index(low[start:], needle)
	if j < 0 {
		return ""
	}
	chunk := s[start : start+j]
	if lt := strings.LastIndex(chunk, "<"); lt >= 0 {
		chunk = chunk[:lt]
	}
	chunk = strings.TrimSpace(chunk)
	chunk = strings.ReplaceAll(chunk, "<", "<")
	chunk = strings.ReplaceAll(chunk, ">", ">")
	chunk = strings.ReplaceAll(chunk, "&", "&")
	return chunk
}

func splitCI(s, name string) []string {
	low := strings.ToLower(s)
	open := "<" + strings.ToLower(name)
	var out []string
	for {
		i := strings.Index(low, open)
		if i < 0 {
			break
		}
		gt := strings.Index(s[i:], ">")
		if gt < 0 {
			break
		}
		start := i + gt + 1
		close := strings.Index(low[start:], strings.ToLower(name))
		if close < 0 {
			break
		}
		out = append(out, s[start:start+close])
		s = s[start+close:]
		low = strings.ToLower(s)
	}
	return out
}

func abs(base, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return base
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	if strings.HasPrefix(href, "/") {
		// keep scheme+host of base
		rest := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
		host := rest
		if i := strings.Index(rest, "/"); i >= 0 {
			host = rest[:i]
		}
		scheme := "https://"
		if strings.HasPrefix(base, "http://") {
			scheme = "http://"
		}
		return scheme + host + href
	}
	return strings.TrimRight(base, "/") + "/" + href
}

func filter(ev []Event, from, to time.Time) []Event {
	if from.IsZero() {
		return ev
	}
	var out []Event
	for _, e := range ev {
		if e.Start.Before(to) && e.End.After(from) {
			out = append(out, e)
		}
	}
	return out
}

func esc(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, ",", `\,`)
	s = strings.ReplaceAll(s, ";", `\;`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
