package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/PacificDailyTimes/pdt-news/internal/config"
	"github.com/PacificDailyTimes/pdt-news/internal/httpx"
)

func main() {
	cfgPath := config.Find()
	var cfg *config.Config
	var err error
	if cfgPath != "" {
		cfg, err = config.Load(cfgPath)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		cfg = &config.Config{Bind: "127.0.0.1", Port: "9001", Mode: "single", MailTransport: "off", WalletDir: "/etc/pdt/wallet", Theme: "masthead"}
	}
	root := findRoot()
	log.Printf("pdt-news listening %s  config=%s  mode=%s", cfg.Addr(), cfgPath, cfg.Mode)
	if err := httpx.Listen(cfg, root); err != nil {
		log.Fatal(err)
	}
}

func findRoot() string {
	cands := []string{"."}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Dir(exe), filepath.Join(filepath.Dir(exe), ".."))
	}
	for _, c := range cands {
		if st, err := os.Stat(filepath.Join(c, "web", "templates")); err == nil && st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	abs, _ := filepath.Abs(".")
	return abs
}
