package tax

// Destination-based combined state+avg local rate, percent. Not a substitute for a CPA.
// Source: typical combined rates rounded; used until a paid lookup is wired.
var state = map[string]float64{
	"AL": 9.29, "AK": 1.76, "AZ": 8.37, "AR": 9.43, "CA": 8.82,
	"CO": 7.77, "CT": 6.35, "DE": 0, "DC": 6.00, "FL": 7.02,
	"GA": 7.35, "HI": 4.44, "ID": 6.03, "IL": 8.81, "IN": 7.00,
	"IA": 6.94, "KS": 8.74, "KY": 6.00, "LA": 9.55, "ME": 5.50,
	"MD": 6.00, "MA": 6.25, "MI": 6.00, "MN": 7.49, "MS": 7.07,
	"MO": 8.25, "MT": 0, "NE": 6.94, "NV": 8.23, "NH": 0,
	"NJ": 6.60, "NM": 7.62, "NY": 8.52, "NC": 6.98, "ND": 6.96,
	"OH": 7.23, "OK": 8.99, "OR": 0, "PA": 6.34, "RI": 7.00,
	"SC": 7.46, "SD": 6.11, "TN": 9.55, "TX": 8.19, "UT": 7.19,
	"VT": 6.36, "VA": 5.75, "WA": 9.38, "WV": 6.50, "WI": 5.43,
	"WY": 5.36,
}

func Cents(subtotalCents int, country, st, zip string) int {
	if country != "" && country != "US" && country != "USA" && country != "United States" {
		return 0
	}
	r, ok := state[stringsUpper(st)]
	if !ok {
		return 0
	}
	_ = zip
	return int(float64(subtotalCents) * r / 100.0)
}

func stringsUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}
