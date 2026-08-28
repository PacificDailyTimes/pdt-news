package caldav

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Discover finds a calendar collection from a host or principal URL.
// iCloud: start at https://caldav.icloud.com/ with Apple ID + app password.
func (a Acc) Discover() (string, error) {
	start := strings.TrimSpace(a.URL)
	switch strings.ToLower(strings.TrimRight(start, "/")) {
	case "", "icloud", "https://icloud.com", "http://icloud.com", "apple":
		start = "https://caldav.icloud.com/"
	}
	if !strings.HasPrefix(start, "http") {
		start = "https://" + start
	}
	if strings.Contains(start, "/calendars/") && strings.Count(start, "/") >= 5 {
		return a.col(), nil
	}
	body, loc, err := a.propfind(start, `<d:propfind xmlns:d="DAV:"><d:prop><d:current-user-principal/></d:prop></d:propfind>`, "0")
	if err != nil {
		return "", err
	}
	princ := abs(loc, firstHref(body, "current-user-principal"))
	if princ == "" {
		princ = start
	}
	body, loc, err = a.propfind(princ, `<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:calendar-home-set/></d:prop></d:propfind>`, "0")
	if err != nil {
		return "", err
	}
	home := abs(loc, firstHref(body, "calendar-home-set"))
	if home == "" {
		return "", fmt.Errorf("no calendar home")
	}
	body, loc, err = a.propfind(home, `<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:displayname/><d:resourcetype/><c:supported-calendar-component-set/></d:prop></d:propfind>`, "1")
	if err != nil {
		return "", err
	}
	for _, ch := range splitCI(body, "response") {
		if !strings.Contains(strings.ToLower(ch), "<c:calendar") && !strings.Contains(strings.ToLower(ch), ":calendar/>") && !strings.Contains(strings.ToLower(ch), "<calendar") {
			continue
		}
		h := inner(ch, "href")
		if h == "" {
			continue
		}
		return abs(loc, h), nil
	}
	return home, nil
}

func (a Acc) propfind(dest, body, depth string) (string, string, error) {
	req, err := http.NewRequest("PROPFIND", dest, strings.NewReader(body))
	if err != nil {
		return "", dest, err
	}
	a.auth(req)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", depth)
	resp, err := httpClient().Do(req)
	if err != nil {
		return "", dest, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", dest, fmt.Errorf("propfind %d", resp.StatusCode)
	}
	final := dest
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	return string(b), final, nil
}

func firstHref(xml, section string) string {
	low := strings.ToLower(xml)
	i := strings.Index(low, strings.ToLower(section))
	if i < 0 {
		return inner(xml, "href")
	}
	return inner(xml[i:], "href")
}
