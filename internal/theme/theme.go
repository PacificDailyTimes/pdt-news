// Package theme lists drop-in CSS files.
//
// Layout lives in pdt.css (mast, wrap, deck, card, thread, dash).
// A theme file may only set CSS variables and the theme- hooks:
//
//	.theme-cta   buttons and landing CTAs
//	.theme-mark  the site title
//	:root        --bg --ink --mute --rule --accent --card --sans --serif --cta-bg --cta-ink
//
// Filename = selector. masthead.css is picked as "masthead" and applied as
// data-theme="masthead" plus body.theme-masthead. Visitors never learn a new UI.
package theme

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func Dir(root string) string {
	return filepath.Join(root, "web", "static", "css", "themes")
}

func List(root string) []string {
	ents, err := os.ReadDir(Dir(root))
	if err != nil {
		return []string{"masthead"}
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".css") {
			continue
		}
		out = append(out, strings.TrimSuffix(n, ".css"))
	}
	sort.Strings(out)
	if len(out) == 0 {
		return []string{"masthead"}
	}
	return out
}

func Known(root, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "/\\.") {
		return false
	}
	st, err := os.Stat(filepath.Join(Dir(root), name+".css"))
	return err == nil && !st.IsDir()
}
