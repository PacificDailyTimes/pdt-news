package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Bind, Port, URL                                    string
	DBHost, DBPort, DBName, DBUser, DBPass             string
	Mode                                               string // single | multi
	SetupPassword, SetupHash                           string
	MailTransport, MailFrom, MailName                  string
	SMTPHost, SMTPPort, SMTPUser, SMTPPass, SMTPSecure string
	StripeSecret, StripePub                            string
	PaypalID, PaypalSecret                             string
	PaypalSandbox                                      bool
	StripeWhsec                                        string
	WalletDir                                          string
	Coins                                              map[string]Coin
	Theme                                              string
	Path                                               string
	GoogleID, GoogleSecret                             string
	AppleID, AppleSecret                               string
	GithubID, GithubSecret                             string
	OAuthClientID, OAuthClientSecret                   string
	BadAdURL, BadAdPub, BadAdSec                       string
	EnableShop, EnableSubs, EnableAppt                 string
}

type Coin struct {
	Ticker  string
	Address string
	KeyFile string
}

func Find() string {
	if p := os.Getenv("PDT_CONFIG"); p != "" {
		return p
	}
	cands := []string{
		"/etc/pdt/config",
		"/srv/www/pdt/config",
		"/var/www/pdt/config",
		"config",
		"config.sample",
	}
	if exe, err := os.Executable(); err == nil {
		cands = append([]string{filepath.Join(filepath.Dir(exe), "config")}, cands...)
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &Config{
		Bind:          "127.0.0.1",
		Port:          "9001",
		Mode:          "single",
		MailTransport: "off",
		WalletDir:     "/etc/pdt/wallet",
		Theme:         "masthead",
		Coins:         map[string]Coin{},
		Path:          path,
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "web_bind":
			c.Bind = v
		case "web_port":
			c.Port = v
		case "web_url":
			c.URL = strings.TrimRight(v, "/")
		case "db_host":
			c.DBHost = v
		case "db_port":
			c.DBPort = v
		case "db_name":
			c.DBName = v
		case "db_user":
			c.DBUser = v
		case "db_pass":
			c.DBPass = v
		case "mode":
			c.Mode = v
		case "setup_password":
			c.SetupPassword = v
		case "setup_password_hash":
			c.SetupHash = v
		case "mail_transport":
			c.MailTransport = v
		case "mail_from":
			c.MailFrom = v
		case "mail_from_name":
			c.MailName = v
		case "smtp_host":
			c.SMTPHost = v
		case "smtp_port":
			c.SMTPPort = v
		case "smtp_user":
			c.SMTPUser = v
		case "smtp_pass":
			c.SMTPPass = v
		case "smtp_secure":
			c.SMTPSecure = v
		case "stripe_secret":
			c.StripeSecret = v
		case "stripe_publishable":
			c.StripePub = v
		case "paypal_client_id":
			c.PaypalID = v
		case "paypal_secret":
			c.PaypalSecret = v
		case "paypal_sandbox":
			c.PaypalSandbox = v == "1" || strings.EqualFold(v, "true")
		case "stripe_webhook_secret":
			c.StripeWhsec = v
		case "wallet_dir":
			c.WalletDir = v
		case "theme":
			c.Theme = v
		case "oauth_google_id":
			c.GoogleID = v
		case "oauth_google_secret":
			c.GoogleSecret = v
		case "oauth_apple_id":
			c.AppleID = v
		case "oauth_apple_secret":
			c.AppleSecret = v
		case "oauth_github_id":
			c.GithubID = v
		case "oauth_github_secret":
			c.GithubSecret = v
		case "oauth_client_id":
			c.OAuthClientID = v
		case "oauth_client_secret":
			c.OAuthClientSecret = v
		case "badad_url":
			c.BadAdURL = strings.TrimRight(v, "/")
		case "badad_pub":
			c.BadAdPub = v
		case "badad_sec":
			c.BadAdSec = v
		case "enable_shop":
			c.EnableShop = v
		case "enable_subs":
			c.EnableSubs = v
		case "enable_appointments":
			c.EnableAppt = v
		default:
			if strings.HasSuffix(k, "_key") {
				t := strings.TrimSuffix(k, "_key")
				coin := c.Coins[t]
				coin.Ticker = t
				coin.KeyFile = v
				c.Coins[t] = coin
			} else if isTicker(k) {
				coin := c.Coins[k]
				coin.Ticker = k
				coin.Address = v
				c.Coins[k] = coin
			}
		}
	}
	return c, nil
}

func isTicker(k string) bool {
	known := "btc eth sol xrp xlm avax hbar usdt usdc dai busd shib lunc volt pepe doge ada dot matic trx ton bnb fil ltc bch xmr algo near apt sui sei inj op arb"
	return strings.Contains(" "+known+" ", " "+strings.ToLower(k)+" ")
}

func (c *Config) DSN() string {
	return "postgres://" + c.DBUser + ":" + c.DBPass + "@" + c.DBHost + ":" + c.DBPort + "/" + c.DBName + "?sslmode=disable"
}

func (c *Config) Addr() string {
	return c.Bind + ":" + c.Port
}

func (c *Config) Multi() bool { return c.Mode == "network" || c.Mode == "multi" }

func (c *Config) SetupOK(given string) bool {
	if c.SetupPassword == "" && c.SetupHash == "" {
		return true
	}
	if c.SetupPassword != "" && given == c.SetupPassword {
		return true
	}
	if c.SetupHash != "" {
		sum := sha256.Sum256([]byte(given))
		return strings.EqualFold(hex.EncodeToString(sum[:]), c.SetupHash)
	}
	return given == "" && c.SetupPassword == "" && c.SetupHash == ""
}

func Write(path string, kv map[string]string) error {
	var b strings.Builder
	b.WriteString("# pdt-news config — generated\n")
	keys := []string{
		"web_bind", "web_port", "web_url",
		"db_host", "db_port", "db_name", "db_user", "db_pass",
		"mode", "mail_transport", "mail_from", "mail_from_name", "theme",
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if v, ok := kv[k]; ok {
			b.WriteString(k + "=" + v + "\n")
			seen[k] = true
		}
	}
	for k, v := range kv {
		if !seen[k] {
			b.WriteString(k + "=" + v + "\n")
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && filepath.Dir(path) != "." {
		// tarball next to binary
	}
	return os.WriteFile(path, []byte(b.String()), 0640)
}

func PortInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
