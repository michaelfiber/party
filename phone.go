package party

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	purposeLogin       = "login"
	authCookieName     = "party_auth"
	phoneTokensFile    = "data/phone-tokens.json"
	otpTTL             = 10 * time.Minute
	authTTL            = 10 * 365 * 24 * time.Hour
	otpSendCooldown    = 45 * time.Second
	otpMaxAttempts     = 5
	otpMaxSendsPerHour = 8
)

type otpRecord struct {
	Hash     [32]byte
	Expires  time.Time
	Attempts int
	Created  time.Time
}

type authRecord struct {
	Phone   string
	Expires time.Time
}

type storedPhoneToken struct {
	Hash    string    `json:"hash"`
	Phone   string    `json:"phone"`
	Expires time.Time `json:"expiresAt"`
}

type partyInfo struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Code    string    `json:"code"`
	Created time.Time `json:"createdAt"`
	Photos  int       `json:"photos"`
}

var (
	phoneMu   sync.Mutex
	otps      = map[string]otpRecord{}
	auths     = map[string]authRecord{}
	lastSend  = map[string]time.Time{}
	sendsByIP = map[string][]time.Time{}
)

func resetPhoneState() {
	phoneMu.Lock()
	otps = map[string]otpRecord{}
	auths = map[string]authRecord{}
	lastSend = map[string]time.Time{}
	sendsByIP = map[string][]time.Time{}
	phoneMu.Unlock()
}

func loadPhoneTokens() {
	resetPhoneState()
	var list []storedPhoneToken
	loadJSON(phoneTokensFile, &list)
	now := time.Now()
	phoneMu.Lock()
	for _, item := range list {
		if item.Hash == "" || item.Phone == "" || now.After(item.Expires) {
			continue
		}
		auths[item.Hash] = authRecord{Phone: item.Phone, Expires: item.Expires}
	}
	phoneMu.Unlock()
}

func persistPhoneTokens() {
	phoneMu.Lock()
	now := time.Now()
	list := make([]storedPhoneToken, 0, len(auths))
	for hash, rec := range auths {
		if hash == "" || rec.Phone == "" || now.After(rec.Expires) {
			delete(auths, hash)
			continue
		}
		list = append(list, storedPhoneToken{Hash: hash, Phone: rec.Phone, Expires: rec.Expires})
	}
	phoneMu.Unlock()
	if err := saveJSON(phoneTokensFile, list); err != nil {
		logf("phone tokens save: %v", err)
	}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func maskPhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if len(phone) < 6 {
		return ""
	}
	return phone[:2] + strings.Repeat("•", len(phone)-6) + phone[len(phone)-4:]
}

func otpKey(phone, purpose string) string {
	return phone + "|" + purpose
}

func hashOTP(phone, purpose, code string) [32]byte {
	return sha256.Sum256([]byte(phone + "|" + purpose + "|" + code))
}

func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func randomAuthToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func prunePhoneState(now time.Time) {
	for k, rec := range otps {
		if now.After(rec.Expires) {
			delete(otps, k)
		}
	}
	for k, rec := range auths {
		if now.After(rec.Expires) {
			delete(auths, k)
		}
	}
	hourAgo := now.Add(-time.Hour)
	for ip, times := range sendsByIP {
		kept := times[:0]
		for _, t := range times {
			if t.After(hourAgo) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(sendsByIP, ip)
		} else {
			sendsByIP[ip] = kept
		}
	}
}

func tooManySends(ip string, now time.Time) bool {
	times := sendsByIP[ip]
	n := 0
	for _, t := range times {
		if now.Sub(t) < time.Hour {
			n++
		}
	}
	return n >= otpMaxSendsPerHour
}

func setAuthCookie(w http.ResponseWriter, token string) {
	if w == nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     cookiePath(),
		MaxAge:   int(authTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearAuthCookie(w http.ResponseWriter) {
	if w == nil {
		return
	}
	seen := map[string]bool{}
	for _, path := range []string{"/", cookiePath()} {
		if seen[path] {
			continue
		}
		seen[path] = true
		http.SetCookie(w, &http.Cookie{
			Name:     authCookieName,
			Value:    "",
			Path:     path,
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

func tokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) >= 7 && strings.EqualFold(auth[:7], "Bearer ") {
		if t := strings.TrimSpace(auth[7:]); t != "" {
			return t
		}
	}
	if t := strings.TrimSpace(r.Header.Get("X-Party-Token")); t != "" {
		return t
	}
	c, err := r.Cookie(authCookieName)
	if err != nil || c == nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}

func phoneForToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	h := hashToken(token)
	phoneMu.Lock()
	defer phoneMu.Unlock()
	rec, ok := auths[h]
	if !ok || time.Now().After(rec.Expires) {
		return ""
	}
	return rec.Phone
}

func recoverPhone(r *http.Request) string {
	return phoneForToken(tokenFromRequest(r))
}

func putAuthToken(token, phone string) {
	phoneMu.Lock()
	auths[hashToken(token)] = authRecord{Phone: phone, Expires: time.Now().Add(authTTL)}
	phoneMu.Unlock()
	persistPhoneTokens()
}

func revokeAuthToken(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	phoneMu.Lock()
	delete(auths, hashToken(token))
	phoneMu.Unlock()
	persistPhoneTokens()
}

func partiesForPhone(phone string) []partyInfo {
	storeMu.Lock()
	defer storeMu.Unlock()
	out := make([]partyInfo, 0)
	for _, p := range parties {
		if p == nil || p.Phone != phone {
			continue
		}
		out = append(out, partyInfo{
			ID:      p.ID,
			Name:    p.Name,
			Code:    p.Code,
			Created: p.Created,
			Photos:  p.PhotoCount,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Created.After(out[j].Created)
	})
	return out
}

func loginStatus(r *http.Request, phone string, p *Party) map[string]any {
	body := map[string]any{
		"ok":         true,
		"needsLogin": false,
		"debug":      debugMode(),
		"sources":    configuredSources(),
		"phone":      maskPhone(phone),
		"parties":    partiesForPhone(phone),
	}
	if p != nil && p.Phone == phone {
		for k, v := range hostStatus(r, p) {
			if k == "ok" || k == "needsParty" {
				continue
			}
			body[k] = v
		}
	}
	return body
}

func handlePhoneSend(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var body struct {
		Phone string `json:"phone"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	phone, err := normalizePhone(body.Phone)
	if err != nil {
		http.Error(w, "enter a valid phone number", http.StatusBadRequest)
		return
	}
	code, err := randomCode()
	if err != nil {
		http.Error(w, "could not make a code", http.StatusInternalServerError)
		return
	}
	now := time.Now()
	ip := clientIP(r)
	phoneMu.Lock()
	prunePhoneState(now)
	if tooManySends(ip, now) {
		phoneMu.Unlock()
		http.Error(w, "too many codes from this network. try later", http.StatusTooManyRequests)
		return
	}
	if last, ok := lastSend[phone]; ok && now.Sub(last) < otpSendCooldown {
		phoneMu.Unlock()
		http.Error(w, "wait a minute before requesting another code", http.StatusTooManyRequests)
		return
	}
	otps[otpKey(phone, purposeLogin)] = otpRecord{
		Hash:     hashOTP(phone, purposeLogin, code),
		Expires:  now.Add(otpTTL),
		Created:  now,
		Attempts: 0,
	}
	lastSend[phone] = now
	sendsByIP[ip] = append(sendsByIP[ip], now)
	phoneMu.Unlock()

	msg := "[party] Your Party Cam code is " + code + ". It expires in 10 minutes."
	if err := sendSMSFn(phone, msg); err != nil {
		if !debugMode() {
			phoneMu.Lock()
			delete(otps, otpKey(phone, purposeLogin))
			phoneMu.Unlock()
			logf("phone code SMS: %v", err)
			http.Error(w, "could not send the text. try again in a moment", http.StatusBadGateway)
			return
		}
		logf("phone code SMS in debug: %v", err)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "debug": true, "debugCode": code})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func consumeOTP(phone, purpose, code string) error {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return fmt.Errorf("enter the 6-digit code")
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return fmt.Errorf("enter the 6-digit code")
		}
	}
	key := otpKey(phone, purpose)
	want := hashOTP(phone, purpose, code)
	now := time.Now()
	phoneMu.Lock()
	defer phoneMu.Unlock()
	prunePhoneState(now)
	rec, ok := otps[key]
	if !ok {
		return fmt.Errorf("request a new code")
	}
	if now.After(rec.Expires) {
		delete(otps, key)
		return fmt.Errorf("that code expired. request a new one")
	}
	rec.Attempts++
	if subtle.ConstantTimeCompare(rec.Hash[:], want[:]) != 1 {
		if rec.Attempts >= otpMaxAttempts {
			delete(otps, key)
			return fmt.Errorf("too many tries. request a new code")
		}
		otps[key] = rec
		return fmt.Errorf("that code does not match")
	}
	delete(otps, key)
	return nil
}

func handlePhoneVerify(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var body struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	phone, err := normalizePhone(body.Phone)
	if err != nil {
		http.Error(w, "enter a valid phone number", http.StatusBadRequest)
		return
	}
	if err := consumeOTP(phone, purposeLogin, body.Code); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	token, err := randomAuthToken()
	if err != nil {
		http.Error(w, "could not start the session", http.StatusInternalServerError)
		return
	}
	putAuthToken(token, phone)
	setAuthCookie(w, token)
	out := loginStatus(r, phone, nil)
	out["token"] = token
	writeJSON(w, http.StatusOK, out)
}

func handlePhoneLogout(w http.ResponseWriter, r *http.Request) {
	revokeAuthToken(tokenFromRequest(r))
	clearAuthCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleListParties(w http.ResponseWriter, r *http.Request) {
	phone := recoverPhone(r)
	if phone == "" {
		http.Error(w, "verify your phone number first", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, loginStatus(r, phone, nil))
}

func handleOpenParty(w http.ResponseWriter, r *http.Request) {
	phone := recoverPhone(r)
	if phone == "" {
		http.Error(w, "verify your phone number first", http.StatusUnauthorized)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	p := getParty(strings.TrimSpace(body.ID))
	if p == nil || p.Phone != phone {
		http.Error(w, "that party is not on this number", http.StatusNotFound)
		return
	}
	setHostCookies(w, p.ID, p.Secret)
	writeJSON(w, http.StatusOK, loginStatus(r, phone, p))
}

func handleDeleteParty(w http.ResponseWriter, r *http.Request) {
	phone := recoverPhone(r)
	if phone == "" {
		http.Error(w, "verify your phone number first", http.StatusUnauthorized)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	p := getParty(strings.TrimSpace(body.ID))
	if p == nil || p.Phone != phone {
		http.Error(w, "that party is not on this number", http.StatusNotFound)
		return
	}
	if err := deleteParty(p.ID); err != nil {
		logf("delete party %s: %v", p.ID, err)
		http.Error(w, "could not delete every photo; try again", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, loginStatus(r, phone, nil))
}
