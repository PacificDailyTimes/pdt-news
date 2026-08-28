// Package caldav talks to a remote CalDAV collection.
// We never host CalDAV. This is a client: Fastmail, Nextcloud, iCloud, Google.
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

func (a Acc) ok() bool { return a.URL != "" }

func (a Acc) Busy(from, to time.Time) [][2]time.Time {
	if !a.ok() {
		return nil
	}
	if ev := a.getICS(); len(ev) > 0 {
		return inRange(ev, from, to)
	}
	return inRange(a.report(from, to), from, to)
}

func (a Acc) Put(uid string, start, end time.Time, summary, desc string) error {
	if !a.ok() {
		return nil
	}
	body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//pdt-news//EN\r\nBEGIN:VEVENT\r\n" +
		"UID:" + uid + "@pdt\r\n" +
		"DTSTAMP:" + start.UTC().Format("20060102T150405Z") + "\r\n" +
		"DTSTART:" + start.UTC().Format("20060102T150405Z") + "\r\n" +
		"DTEND:" + end.UTC().Format("20060102T150405Z") + "\r\n" +
		"SUMMARY:" + esc(summary) + "\r\n" +
		"DESCRIPTION:" + esc(desc) + "\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	dest := strings.TrimRight(a.URL, "/") + "/" + uid + ".ics"
	req, err := http.NewRequest(http.MethodPut, dest, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")
	a.auth(req)
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("caldav put %d: %s", resp.StatusCode, b)
	}
	return nil
}

func (a Acc) auth(r *http.Request) {
	if a.User != "" {
		r.SetBasicAuth(a.User, a.Pass)
	}
}

func (a Acc) getICS() [][2]time.Time {
	req, err := http.NewRequest(http.MethodGet, a.URL, nil)
	if err != nil {
		return nil
	}
	a.auth(req)
	req.Header.Set("Accept", "text/calendar, text/plain, */*")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if !strings.Contains(string(b), "BEGIN:VCALENDAR") {
		return nil
	}
	return ParseICS(string(b))
}

func (a Acc) report(from, to time.Time) [][2]time.Time {
	q := `<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><c:calendar-data/></d:prop>
  <c:filter>
    <c:comp-filter name="VCALENDAR">
      <c:comp-filter name="VEVENT">
        <c:time-range start="` + from.UTC().Format("20060102T150405Z") + `" end="` + to.UTC().Format("20060102T150405Z") + `"/>
      </c:comp-filter>
    </c:comp-filter>
  </c:filter>
</c:calendar-query>`
	req, err := http.NewRequest("REPORT", a.URL, strings.NewReader(q))
	if err != nil {
		return nil
	}
	a.auth(req)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", "1")
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return ParseICS(extractCalData(string(b)))
}

func extractCalData(xml string) string {
	var b strings.Builder
	low := strings.ToLower(xml)
	for {
		i := strings.Index(low, "calendar-data")
		if i < 0 {
			break
		}
		gt := strings.Index(xml[i:], ">")
		if gt < 0 {
			break
		}
		start := i + gt + 1
		endTag := strings.Index(low[start:], "calendar-data")
		if endTag < 0 {
			break
		}
		chunk := xml[start : start+endTag]
		if lt := strings.LastIndex(chunk, "<"); lt >= 0 {
			chunk = chunk[:lt]
		}
		b.WriteString(chunk)
		b.WriteByte('\n')
		xml = xml[start+endTag:]
		low = strings.ToLower(xml)
	}
	if b.Len() == 0 {
		return xml
	}
	return b.String()
}

func ParseICS(s string) [][2]time.Time {
	var out [][2]time.Time
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var start, end time.Time
	in := false
	for _, ln := range lines {
		if ln == "BEGIN:VEVENT" {
			in, start, end = true, time.Time{}, time.Time{}
		}
		if !in {
			continue
		}
		if strings.HasPrefix(ln, "DTSTART") {
			start = parseICSTime(ln)
		}
		if strings.HasPrefix(ln, "DTEND") {
			end = parseICSTime(ln)
		}
		if ln == "END:VEVENT" {
			if !start.IsZero() {
				if end.IsZero() {
					end = start.Add(30 * time.Minute)
				}
				out = append(out, [2]time.Time{start, end})
			}
			in = false
		}
	}
	return out
}

func parseICSTime(ln string) time.Time {
	i := strings.LastIndex(ln, ":")
	if i < 0 {
		return time.Time{}
	}
	v := strings.TrimSpace(ln[i+1:])
	v = strings.TrimSuffix(v, "Z")
	for _, layout := range []string{"20060102T150405", "20060102"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

func inRange(ev [][2]time.Time, from, to time.Time) [][2]time.Time {
	if from.IsZero() {
		return ev
	}
	var out [][2]time.Time
	for _, e := range ev {
		if e[0].Before(to) && e[1].After(from) {
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
