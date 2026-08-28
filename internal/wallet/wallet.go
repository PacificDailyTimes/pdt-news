package wallet

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PacificDailyTimes/pdt-news/internal/config"
)

type Row struct {
	Ticker  string
	Address string
	HasKey  bool
	Balance string
	Err     string
}

func Dir(cfg *config.Config) string {
	if cfg.WalletDir != "" {
		return cfg.WalletDir
	}
	return "/etc/pdt/wallet"
}

func List(cfg *config.Config) []Row {
	out := []Row{}
	for t, c := range cfg.Coins {
		r := Row{Ticker: strings.ToUpper(t), Address: c.Address}
		kf := c.KeyFile
		if kf == "" {
			kf = filepath.Join(Dir(cfg), strings.ToLower(t)+".key")
		}
		if st, err := os.Stat(kf); err == nil && !st.IsDir() {
			r.HasKey = true
		}
		r.Balance, r.Err = lookup(t, c.Address)
		out = append(out, r)
	}
	// also pick up loose key files
	ents, _ := os.ReadDir(Dir(cfg))
	have := map[string]bool{}
	for _, r := range out {
		have[strings.ToLower(r.Ticker)] = true
	}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".key") {
			continue
		}
		t := strings.TrimSuffix(e.Name(), ".key")
		if have[t] {
			continue
		}
		out = append(out, Row{Ticker: strings.ToUpper(t), HasKey: true, Address: "(set address in config)"})
	}
	return out
}

func lookup(ticker, addr string) (string, string) {
	if addr == "" {
		return "", ""
	}
	client := &http.Client{Timeout: 6 * time.Second}
	t := strings.ToLower(ticker)
	var url string
	switch t {
	case "btc":
		url = "https://blockstream.info/api/address/" + addr
	case "eth", "usdt", "usdc", "shib", "pepe":
		url = "https://api.etherscan.io/api?module=account&action=balance&address=" + addr + "&tag=latest"
	default:
		return "—", ""
	}
	resp, err := client.Get(url)
	if err != nil {
		return "—", err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return strings.TrimSpace(string(b)), ""
	}
	if t == "btc" {
		if chain, ok := m["chain_stats"].(map[string]any); ok {
			funded, _ := chain["funded_txo_sum"].(float64)
			spent, _ := chain["spent_txo_sum"].(float64)
			return fmt.Sprintf("%.8f BTC", (funded-spent)/1e8), ""
		}
	}
	if r, ok := m["result"].(string); ok {
		return r + " wei", ""
	}
	return "—", ""
}

func NeverServe(path string) bool {
	return strings.Contains(path, "/etc/pdt/") || strings.HasSuffix(path, ".key")
}
