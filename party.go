package party

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const (
	hostCookieName   = "party_id"
	secretCookieName = "party_secret"
	hostCookieAge    = 10 * 365 * 24 * 60 * 60
	codeAlphabet     = "abcdefghjkmnpqrstuvwxyz23456789"
)

type Account struct {
	Source       string    `json:"source"`
	UserID       string    `json:"userId"`
	UserName     string    `json:"userName"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
}

type Party struct {
	ID         string    `json:"id"`
	Code       string    `json:"code"`
	Secret     string    `json:"secret"`
	Name       string    `json:"name"`
	Created    time.Time `json:"createdAt"`
	Account    *Account  `json:"account,omitempty"`
	FolderID   string    `json:"folderId,omitempty"`
	FolderName string    `json:"folderName,omitempty"`
	FolderPath string    `json:"folderPath,omitempty"`
	PhotoCount int       `json:"photoCount"`
}

type Shot struct {
	ID       string    `json:"id"`
	PartyID  string    `json:"partyId"`
	Name     string    `json:"name"`
	Facing   string    `json:"facing"`
	Path     string    `json:"path"`
	CloudOK  bool      `json:"cloudOk"`
	CloudErr string    `json:"cloudErr,omitempty"`
	Created  time.Time `json:"createdAt"`
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hexs := hex.EncodeToString(b)
	return hexs[0:8] + "-" + hexs[8:12] + "-" + hexs[12:16] + "-" + hexs[16:20] + "-" + hexs[20:32]
}

func newPartyCode() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return strings.ToLower(hex.EncodeToString([]byte(time.Now().Format("150405")))[:8])
	}
	out := make([]byte, 8)
	for i := range out {
		out[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(out)
}

func setHostCookies(w http.ResponseWriter, partyID, secret string) {
	if w == nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     hostCookieName,
		Value:    partyID,
		Path:     cookiePath(),
		MaxAge:   hostCookieAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     secretCookieName,
		Value:    secret,
		Path:     cookiePath(),
		MaxAge:   hostCookieAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func readCookie(r *http.Request, name string) string {
	if r == nil {
		return ""
	}
	c, err := r.Cookie(name)
	if err != nil || c == nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}

func hostParty(w http.ResponseWriter, r *http.Request) *Party {
	id := readCookie(r, hostCookieName)
	secret := readCookie(r, secretCookieName)
	p := getParty(id)
	if p == nil || p.Secret == "" || secret == "" {
		if w != nil {
			http.Error(w, "start a party first", http.StatusUnauthorized)
		}
		return nil
	}
	a := []byte(p.Secret)
	b := []byte(secret)
	if len(a) != len(b) || subtle.ConstantTimeCompare(a, b) != 1 {
		if w != nil {
			http.Error(w, "invalid party session", http.StatusUnauthorized)
		}
		return nil
	}
	setHostCookies(w, p.ID, p.Secret)
	return p
}

func existingHost(r *http.Request) *Party {
	return hostParty(nil, r)
}

func mintParty(w http.ResponseWriter, name string) *Party {
	now := time.Now().UTC()
	p := &Party{
		ID:      newID(),
		Code:    newPartyCode(),
		Secret:  newID(),
		Name:    strings.TrimSpace(name),
		Created: now,
	}
	if p.Name == "" {
		p.Name = defaultPartyName(now)
	}
	putParty(p)
	setHostCookies(w, p.ID, p.Secret)
	return p
}

func defaultPartyName(t time.Time) string {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		loc = time.FixedZone("EST", -5*3600)
	}
	return t.In(loc).Format("Party, Jan 2, 2006, 3:04 PM")
}

func joinURL(r *http.Request, code string) string {
	return publicBaseURL(r) + "/p/" + code
}
