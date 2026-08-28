// Package flags is the SysAdmin lock vs site-admin toggle.
//
// Config empty  → site admin may turn the feature on or off.
// Config 0/off  → locked off.
// Config 1/on   → locked on.
package flags

import "strings"

type Flag struct {
	On     bool
	Locked bool
}

type Set struct {
	Shop, Subs, Appointments Flag
}

func Parse(v string) (on bool, locked bool, known bool) {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return false, false, false
	}
	switch v {
	case "0", "false", "off", "no":
		return false, true, true
	case "1", "true", "on", "yes":
		return true, true, true
	}
	return false, false, false
}

func Merge(cfgVal, siteVal string) Flag {
	if on, locked, known := Parse(cfgVal); known {
		return Flag{On: on, Locked: locked}
	}
	on, _, _ := Parse(siteVal)
	if siteVal == "" {
		on = true // friendly default: on, until someone turns it off
	}
	if siteVal == "0" || strings.EqualFold(siteVal, "off") || strings.EqualFold(siteVal, "false") {
		on = false
	}
	if siteVal == "1" || strings.EqualFold(siteVal, "on") || strings.EqualFold(siteVal, "true") {
		on = true
	}
	return Flag{On: on, Locked: false}
}
