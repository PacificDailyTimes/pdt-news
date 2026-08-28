// Package pay is the one checkout workflow for pdt-news and (copied) badAd.
//
//	Stripe  — one-time (Checkout payment) or auto-renewing subscription
//	PayPal  — one-time (Orders) or auto-renewing subscription
//	crypto  — prepaid only, never renews
package pay

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Intent struct {
	OrderID     string
	AmountCents int
	Currency    string
	Title       string
	Email       string
	IntervalN   int    // 1
	Interval    string // day|week|month|year; empty = one-shot product
	Renew       bool   // false for crypto always
	SuccessURL  string
	CancelURL   string
}

type Coin struct {
	Ticker, Address string
}

type Session struct {
	Redirect string
	Ref      string
	Coins    []Coin
	Prepaid  bool
}

type Cfg struct {
	StripeSecret, StripePub string
	PaypalID, PaypalSecret  string
	PaypalSandbox           bool
	Origin                  string
	Coins                   []Coin
}

func Recurring(via string, kind string, wantRenew bool) bool {
	if via == "crypto" || via == "invoice" {
		return false
	}
	if !wantRenew {
		return false
	}
	return kind == "subscription" || kind == "membership"
}

func StripeSession(c Cfg, in Intent) (Session, error) {
	if c.StripeSecret == "" {
		return Session{}, fmt.Errorf("stripe is not configured")
	}
	form := url.Values{
		"mode":                                   {stripeMode(in)},
		"success_url":                            {in.SuccessURL},
		"cancel_url":                             {in.CancelURL},
		"client_reference_id":                    {in.OrderID},
		"customer_email":                         {in.Email},
		"line_items[0][quantity]":                {"1"},
		"line_items[0][price_data][currency]":    {strings.ToLower(nz(in.Currency, "usd"))},
		"line_items[0][price_data][unit_amount]": {fmt.Sprintf("%d", in.AmountCents)},
		"line_items[0][price_data][product_data][name]": {in.Title},
	}
	if in.Renew && in.Interval != "" {
		form.Set("line_items[0][price_data][recurring][interval]", stripeInterval(in.Interval))
		form.Set("line_items[0][price_data][recurring][interval_count]", fmt.Sprintf("%d", max(in.IntervalN, 1)))
	}
	b, err := stripePost(c.StripeSecret, "https://api.stripe.com/v1/checkout/sessions", form)
	if err != nil {
		return Session{}, err
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	url, _ := m["url"].(string)
	id, _ := m["id"].(string)
	if url == "" {
		return Session{}, fmt.Errorf("stripe: %s", string(b))
	}
	return Session{Redirect: url, Ref: id, Prepaid: !in.Renew}, nil
}

func stripeMode(in Intent) string {
	if in.Renew && in.Interval != "" {
		return "subscription"
	}
	return "payment"
}

func stripeInterval(u string) string {
	switch strings.ToLower(u) {
	case "day", "week", "month", "year":
		return strings.ToLower(u)
	default:
		return "month"
	}
}

func stripePost(secret, u string, form url.Values) ([]byte, error) {
	req, _ := http.NewRequest("POST", u, strings.NewReader(form.Encode()))
	req.SetBasicAuth(secret, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return do(req)
}

func PayPalSession(c Cfg, in Intent) (Session, error) {
	if c.PaypalID == "" || c.PaypalSecret == "" {
		return Session{}, fmt.Errorf("paypal is not configured")
	}
	token, err := paypalToken(c)
	if err != nil {
		return Session{}, err
	}
	host := paypalHost(c)
	if in.Renew && in.Interval != "" {
		return paypalSub(c, token, host, in)
	}
	body := map[string]any{
		"intent": "CAPTURE",
		"purchase_units": []map[string]any{{
			"reference_id": in.OrderID,
			"description":  in.Title,
			"amount": map[string]any{
				"currency_code": strings.ToUpper(nz(in.Currency, "USD")),
				"value":         fmt.Sprintf("%.2f", float64(in.AmountCents)/100),
			},
		}},
		"application_context": map[string]any{
			"return_url": in.SuccessURL,
			"cancel_url": in.CancelURL,
			"brand_name": "checkout",
		},
	}
	b, err := paypalJSON(token, "POST", host+"/v2/checkout/orders", body)
	if err != nil {
		return Session{}, err
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	id, _ := m["id"].(string)
	return Session{Redirect: paypalLink(m, "approve"), Ref: id, Prepaid: true}, nil
}

func paypalSub(c Cfg, token, host string, in Intent) (Session, error) {
	prod, err := paypalJSON(token, "POST", host+"/v1/catalogs/products", map[string]any{
		"name": in.Title, "type": "SERVICE", "category": "SOFTWARE",
	})
	if err != nil {
		return Session{}, err
	}
	var pm map[string]any
	_ = json.Unmarshal(prod, &pm)
	pid, _ := pm["id"].(string)
	plan, err := paypalJSON(token, "POST", host+"/v1/billing/plans", map[string]any{
		"product_id": pid,
		"name":       in.Title + " plan",
		"billing_cycles": []map[string]any{{
			"frequency":      map[string]any{"interval_unit": paypalUnit(in.Interval), "interval_count": max(in.IntervalN, 1)},
			"tenure_type":    "REGULAR",
			"sequence":       1,
			"total_cycles":   0,
			"pricing_scheme": map[string]any{"fixed_price": map[string]any{"value": fmt.Sprintf("%.2f", float64(in.AmountCents)/100), "currency_code": strings.ToUpper(nz(in.Currency, "USD"))}},
		}},
		"payment_preferences": map[string]any{"auto_bill_outstanding": true},
	})
	if err != nil {
		return Session{}, err
	}
	var pl map[string]any
	_ = json.Unmarshal(plan, &pl)
	planID, _ := pl["id"].(string)
	sub, err := paypalJSON(token, "POST", host+"/v1/billing/subscriptions", map[string]any{
		"plan_id":   planID,
		"custom_id": in.OrderID,
		"application_context": map[string]any{
			"return_url": in.SuccessURL,
			"cancel_url": in.CancelURL,
		},
	})
	if err != nil {
		return Session{}, err
	}
	var sm map[string]any
	_ = json.Unmarshal(sub, &sm)
	return Session{Redirect: paypalLink(sm, "approve"), Ref: str(sm["id"]), Prepaid: false}, nil
}

func paypalUnit(u string) string {
	switch strings.ToLower(u) {
	case "day":
		return "DAY"
	case "week":
		return "WEEK"
	case "year":
		return "YEAR"
	default:
		return "MONTH"
	}
}

func paypalLink(m map[string]any, rel string) string {
	links, _ := m["links"].([]any)
	for _, raw := range links {
		lm, _ := raw.(map[string]any)
		if str(lm["rel"]) == rel {
			return str(lm["href"])
		}
	}
	return ""
}

func paypalHost(c Cfg) string {
	if c.PaypalSandbox {
		return "https://api-m.sandbox.paypal.com"
	}
	return "https://api-m.paypal.com"
}

func paypalToken(c Cfg) (string, error) {
	req, _ := http.NewRequest("POST", paypalHost(c)+"/v1/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	req.SetBasicAuth(c.PaypalID, c.PaypalSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	b, err := do(req)
	if err != nil {
		return "", err
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	t, _ := m["access_token"].(string)
	if t == "" {
		return "", fmt.Errorf("paypal token: %s", string(b))
	}
	return t, nil
}

func paypalJSON(token, method, u string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, u, rdr)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return do(req)
}

func CapturePayPal(c Cfg, orderID string) error {
	token, err := paypalToken(c)
	if err != nil {
		return err
	}
	_, err = paypalJSON(token, "POST", paypalHost(c)+"/v2/checkout/orders/"+orderID+"/capture", map[string]any{})
	return err
}

func CryptoSession(c Cfg, in Intent) Session {
	return Session{Coins: c.Coins, Prepaid: true, Ref: in.OrderID}
}

func PeriodEnd(n int, unit string) time.Time {
	if n < 1 {
		n = 1
	}
	now := time.Now()
	switch strings.ToLower(unit) {
	case "day":
		return now.AddDate(0, 0, n)
	case "week":
		return now.AddDate(0, 0, 7*n)
	case "year":
		return now.AddDate(n, 0, 0)
	default:
		return now.AddDate(0, n, 0)
	}
}

func do(req *http.Request) ([]byte, error) {
	req.Header.Set("User-Agent", "pdt-pay")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return b, fmt.Errorf("%s: %s", resp.Status, string(b))
	}
	return b, nil
}

func nz(a, b string) string {
	if a == "" {
		return b
	}
	return a
}
func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
