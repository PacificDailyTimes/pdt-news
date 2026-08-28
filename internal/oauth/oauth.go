package oauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/PacificDailyTimes/pdt-news/internal/config"
)

type Profile struct {
	Provider, Subject, Email, Name string
}

func StartURL(cfg *config.Config, provider, state, callback string) (string, error) {
	switch provider {
	case "google":
		if cfg.GoogleID == "" {
			return "", fmt.Errorf("google oauth off")
		}
		q := url.Values{
			"client_id":     {cfg.GoogleID},
			"redirect_uri":  {callback},
			"response_type": {"code"},
			"scope":         {"openid email profile"},
			"state":         {state},
		}
		return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode(), nil
	case "github":
		if cfg.GithubID == "" {
			return "", fmt.Errorf("github oauth off")
		}
		q := url.Values{
			"client_id":    {cfg.GithubID},
			"redirect_uri": {callback},
			"scope":        {"user:email"},
			"state":        {state},
		}
		return "https://github.com/login/oauth/authorize?" + q.Encode(), nil
	case "apple":
		if cfg.AppleID == "" {
			return "", fmt.Errorf("apple oauth off")
		}
		q := url.Values{
			"client_id":     {cfg.AppleID},
			"redirect_uri":  {callback},
			"response_type": {"code"},
			"response_mode": {"query"},
			"scope":         {"name email"},
			"state":         {state},
		}
		return "https://appleid.apple.com/auth/authorize?" + q.Encode(), nil
	default:
		return "", fmt.Errorf("unknown provider")
	}
}

func GoogleCalStart(cfg *config.Config, state, callback string) (string, error) {
	if cfg.GoogleID == "" {
		return "", fmt.Errorf("google oauth off")
	}
	q := url.Values{
		"client_id":     {cfg.GoogleID},
		"redirect_uri":  {callback},
		"response_type": {"code"},
		"scope":         {"https://www.googleapis.com/auth/calendar"},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {state},
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode(), nil
}

func GoogleExchange(cfg *config.Config, code, callback string) (access, refresh string, err error) {
	tok, err := postForm("https://oauth2.googleapis.com/token", url.Values{
		"code":          {code},
		"client_id":     {cfg.GoogleID},
		"client_secret": {cfg.GoogleSecret},
		"redirect_uri":  {callback},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		return "", "", err
	}
	return str(tok["access_token"]), str(tok["refresh_token"]), nil
}

func Finish(cfg *config.Config, provider, code, callback string) (*Profile, error) {
	switch provider {
	case "google":
		tok, err := postForm("https://oauth2.googleapis.com/token", url.Values{
			"code":          {code},
			"client_id":     {cfg.GoogleID},
			"client_secret": {cfg.GoogleSecret},
			"redirect_uri":  {callback},
			"grant_type":    {"authorization_code"},
		})
		if err != nil {
			return nil, err
		}
		info, err := getJSON("https://openidconnect.googleapis.com/v1/userinfo", str(tok["access_token"]))
		if err != nil {
			return nil, err
		}
		return &Profile{Provider: "google", Subject: str(info["sub"]), Email: str(info["email"]), Name: str(info["name"])}, nil
	case "github":
		tok, err := postForm("https://github.com/login/oauth/access_token", url.Values{
			"code":          {code},
			"client_id":     {cfg.GithubID},
			"client_secret": {cfg.GithubSecret},
			"redirect_uri":  {callback},
		})
		if err != nil {
			return nil, err
		}
		info, err := getJSON("https://api.github.com/user", str(tok["access_token"]))
		if err != nil {
			return nil, err
		}
		email := str(info["email"])
		if email == "" {
			emails, _ := getJSONArr("https://api.github.com/user/emails", str(tok["access_token"]))
			for _, e := range emails {
				if b, _ := e["primary"].(bool); b {
					email = str(e["email"])
					break
				}
			}
		}
		return &Profile{Provider: "github", Subject: fmt.Sprint(info["id"]), Email: email, Name: first(str(info["name"]), str(info["login"]))}, nil
	case "apple":
		tok, err := postForm("https://appleid.apple.com/auth/token", url.Values{
			"code":          {code},
			"client_id":     {cfg.AppleID},
			"client_secret": {cfg.AppleSecret},
			"redirect_uri":  {callback},
			"grant_type":    {"authorization_code"},
		})
		if err != nil {
			return nil, err
		}
		claims := jwtPayload(str(tok["id_token"]))
		return &Profile{Provider: "apple", Subject: str(claims["sub"]), Email: str(claims["email"]), Name: str(claims["email"])}, nil
	}
	return nil, fmt.Errorf("unknown provider")
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func postForm(u string, v url.Values) (map[string]any, error) {
	req, _ := http.NewRequest("POST", u, strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		q, _ := url.ParseQuery(string(b))
		m = map[string]any{}
		for k := range q {
			m[k] = q.Get(k)
		}
	}
	return m, nil
}

func getJSON(u, token string) (map[string]any, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "pdt-news")
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

func getJSONArr(u, token string) ([]map[string]any, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "pdt-news")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m []map[string]any
	_ = json.Unmarshal(b, &m)
	return m, nil
}

func jwtPayload(jwt string) map[string]any {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return map[string]any{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		b, err2 := base64.URLEncoding.DecodeString(parts[1])
		if err2 != nil {
			return map[string]any{}
		}
		raw = b
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}
