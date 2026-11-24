package party

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

const (
	defaultDebugLoginSMS = "7326701359"
	debugSMSPrefix       = "[party] "
)

var sendSMSFn = sendSMS

func notifyDebugSMS(msg string) {
	if !debugMode() {
		return
	}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	if !strings.HasPrefix(msg, "[") {
		msg = debugSMSPrefix + msg
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				logf("debug SMS: panic %v", rec)
			}
		}()
		if err := sendSMSFn(debugNotifyNumber(), msg); err != nil {
			logf("debug SMS: %v", err)
		}
	}()
}

func notifyDebugNewParty(r *http.Request, p *Party) {
	notifyDebugSMS(debugNewPartyMessage(r, p))
}

func notifyDebugSourceLogin(acc Account) {
	if acc.Source == "" {
		return
	}
	notifyDebugSMS(debugSourceLoginMessage(acc))
}

func debugNewPartyMessage(r *http.Request, p *Party) string {
	name := "a party"
	if p != nil && strings.TrimSpace(p.Name) != "" {
		name = strings.TrimSpace(p.Name)
	}
	return "New party " + name + " from " + clientIP(r)
}

func debugSourceLoginMessage(acc Account) string {
	who := strings.TrimSpace(acc.UserName)
	if who == "" {
		who = strings.TrimSpace(acc.UserID)
	}
	if who == "" {
		who = "someone"
	}
	return fmt.Sprintf("%s logged into %s", who, sourceDisplayName(acc.Source))
}

func debugNotifyNumber() string {
	raw := env("PARTY_DEBUG_LOGIN_SMS")
	if raw == "" {
		raw = defaultDebugLoginSMS
	}
	n, err := normalizePhone(raw)
	if err != nil {
		return "+1" + defaultDebugLoginSMS
	}
	return n
}

func sendSMS(toE164, body string) error {
	sid := env("TWILIO_ACCOUNT_SID")
	token := env("TWILIO_AUTH_TOKEN")
	from := env("TWILIO_FROM_NUMBER")
	if sid == "" || token == "" || from == "" {
		return fmt.Errorf("twilio is not configured")
	}
	form := url.Values{}
	form.Set("To", toE164)
	form.Set("From", from)
	form.Set("Body", body)
	req, err := http.NewRequest(http.MethodPost, "https://api.twilio.com/2010-04-01/Accounts/"+sid+"/Messages.json", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(sid, token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, bytesPreview(raw))
	}
	return nil
}

func normalizePhone(raw string) (string, error) {
	var digits strings.Builder
	for _, r := range raw {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
		}
	}
	n := digits.String()
	if len(n) == 10 {
		n = "1" + n
	}
	if len(n) < 11 || len(n) > 15 {
		return "", fmt.Errorf("invalid phone")
	}
	return "+" + n, nil
}

func clientIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	var candidates []string
	for _, key := range []string{"CF-Connecting-IP", "True-Client-IP", "X-Real-IP"} {
		if v := r.Header.Get(key); v != "" {
			candidates = append(candidates, v)
		}
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		candidates = append(candidates, strings.Split(xff, ",")[0])
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		candidates = append(candidates, host)
	} else if r.RemoteAddr != "" {
		candidates = append(candidates, r.RemoteAddr)
	}
	var fallback string
	for _, raw := range candidates {
		ip := strings.TrimSpace(raw)
		if ip == "" {
			continue
		}
		if parsed := net.ParseIP(ip); parsed != nil {
			if !parsed.IsLoopback() && !parsed.IsUnspecified() {
				return parsed.String()
			}
			if fallback == "" {
				fallback = parsed.String()
			}
		}
	}
	if fallback != "" {
		return fallback
	}
	return "unknown"
}
