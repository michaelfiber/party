package party

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	sourceGoogle   = "google"
	sourceDropbox  = "dropbox"
	sourceOneDrive = "onedrive"
	oauthCookie    = "party_oauth"
)

type oauth2Spec struct {
	AuthURL  string
	TokenURL string
	Scopes   string
	Extra    url.Values
}

func env(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

func debugMode() bool {
	return !strings.EqualFold(env("PARTY_ENV"), "production")
}

const hostedBaseURL = "https://cubeofb.org/party"

func publicBaseURL(r *http.Request) string {
	if v := env("PARTY_PUBLIC_BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	if hostedOnCube(r) {
		return hostedBaseURL
	}
	scheme := "http"
	if r != nil && r.TLS != nil {
		scheme = "https"
	}
	if r != nil {
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			scheme = proto
		}
		host := r.Host
		if host == "" {
			host = "127.0.0.1:3066"
		}
		return scheme + "://" + host
	}
	return "http://127.0.0.1:3066"
}

func hostedOnCube(r *http.Request) bool {
	if r == nil {
		return false
	}
	host := strings.ToLower(r.Host)
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	return host == "cubeofb.org" || host == "www.cubeofb.org"
}

func pathFromBase(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	p := strings.TrimRight(u.Path, "/")
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func publicPath() string {
	if v := env("PARTY_PUBLIC_BASE_URL"); v != "" {
		return pathFromBase(v)
	}
	return ""
}

func publicHome() string {
	if p := publicPath(); p != "" {
		return p + "/"
	}
	return "/"
}

func cookiePath() string {
	if p := publicPath(); p != "" {
		return p
	}
	return "/"
}

func oauthRedirectURL(r *http.Request, source string) string {
	return publicBaseURL(r) + "/api/oauth/" + source + "/callback"
}

func newState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func newPKCE() (verifier, challenge string) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		b = []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge
}

func setOAuthCookie(w http.ResponseWriter, state, source, partyID, verifier string) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthCookie,
		Value:    source + ":" + state + ":" + partyID + ":" + verifier,
		Path:     cookiePath(),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
}

func readOAuthCookie(r *http.Request) (source, state, partyID, verifier string, ok bool) {
	c, err := r.Cookie(oauthCookie)
	if err != nil || c.Value == "" {
		return "", "", "", "", false
	}
	parts := strings.SplitN(c.Value, ":", 4)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", "", false
	}
	source, state = parts[0], parts[1]
	if len(parts) >= 3 {
		partyID = parts[2]
	}
	if len(parts) >= 4 {
		verifier = parts[3]
	}
	return source, state, partyID, verifier, true
}

func clearOAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Path: cookiePath(), MaxAge: -1})
}

func googleOAuthSpec() oauth2Spec {
	return oauth2Spec{
		AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL: "https://oauth2.googleapis.com/token",
		Scopes: strings.Join([]string{
			"https://www.googleapis.com/auth/drive",
			"openid",
			"email",
			"profile",
		}, " "),
		Extra: url.Values{"access_type": {"offline"}, "prompt": {"consent"}},
	}
}

func dropboxOAuthSpec() oauth2Spec {
	return oauth2Spec{
		AuthURL:  "https://www.dropbox.com/oauth2/authorize",
		TokenURL: "https://api.dropboxapi.com/oauth2/token",
		Scopes:   "account_info.read files.content.read files.content.write files.metadata.read files.metadata.write",
		Extra:    url.Values{"token_access_type": {"offline"}},
	}
}

func oneDriveOAuthSpec() oauth2Spec {
	return oauth2Spec{
		AuthURL:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		Scopes:   "offline_access User.Read Files.ReadWrite",
	}
}

func clientCreds(source string) (id, secret string) {
	switch source {
	case sourceGoogle:
		return env("PARTY_GOOGLE_CLIENT_ID"), env("PARTY_GOOGLE_CLIENT_SECRET")
	case sourceDropbox:
		return env("PARTY_DROPBOX_APP_KEY"), env("PARTY_DROPBOX_APP_SECRET")
	case sourceOneDrive:
		return env("PARTY_MICROSOFT_CLIENT_ID"), env("PARTY_MICROSOFT_CLIENT_SECRET")
	}
	return "", ""
}

func sourceConfigured(source string) bool {
	id, secret := clientCreds(source)
	return id != "" && secret != ""
}

func configuredSources() map[string]bool {
	return map[string]bool{
		sourceGoogle:   sourceConfigured(sourceGoogle),
		sourceDropbox:  sourceConfigured(sourceDropbox),
		sourceOneDrive: sourceConfigured(sourceOneDrive),
	}
}

func oauthSpec(source string) (oauth2Spec, bool) {
	switch source {
	case sourceGoogle:
		return googleOAuthSpec(), true
	case sourceDropbox:
		return dropboxOAuthSpec(), true
	case sourceOneDrive:
		return oneDriveOAuthSpec(), true
	default:
		return oauth2Spec{}, false
	}
}

func sourceDisplayName(source string) string {
	switch source {
	case sourceGoogle:
		return "Google Drive"
	case sourceDropbox:
		return "Dropbox"
	case sourceOneDrive:
		return "OneDrive"
	default:
		return source
	}
}

func handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	p := hostParty(w, r)
	if p == nil {
		return
	}
	source := r.PathValue("source")
	startOAuth(w, r, source, p.ID)
}

func startOAuth(w http.ResponseWriter, r *http.Request, source, partyID string) {
	spec, ok := oauthSpec(source)
	if !ok {
		http.Error(w, "unknown cloud", http.StatusNotFound)
		return
	}
	id, secret := clientCreds(source)
	if id == "" || secret == "" {
		http.Error(w, "this cloud is not configured on the server", http.StatusBadRequest)
		return
	}
	state := newState()
	verifier, challenge := newPKCE()
	setOAuthCookie(w, state, source, partyID, verifier)
	q := url.Values{}
	q.Set("client_id", id)
	q.Set("redirect_uri", oauthRedirectURL(r, source))
	q.Set("response_type", "code")
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if spec.Scopes != "" {
		q.Set("scope", spec.Scopes)
	}
	for k, vs := range spec.Extra {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	http.Redirect(w, r, spec.AuthURL+"?"+q.Encode(), http.StatusFound)
}

func handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	finishOAuth(w, r, r.PathValue("source"))
}

func finishOAuth(w http.ResponseWriter, r *http.Request, source string) {
	wantSource, wantState, partyID, verifier, ok := readOAuthCookie(r)
	if !ok || wantSource != source {
		http.Error(w, "oauth state missing", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != wantState {
		http.Error(w, "oauth state mismatch", http.StatusBadRequest)
		return
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		clearOAuthCookie(w)
		redirectOAuthError(w, r, errMsg)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	spec, ok := oauthSpec(source)
	if !ok {
		http.Error(w, "unknown cloud", http.StatusNotFound)
		return
	}
	id, secret := clientCreds(source)
	form := url.Values{}
	form.Set("client_id", id)
	form.Set("client_secret", secret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", oauthRedirectURL(r, source))
	if spec.Scopes != "" {
		form.Set("scope", spec.Scopes)
	}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	tok, err := postToken(spec.TokenURL, form)
	if err != nil {
		logf("oauth token %s: %v", source, err)
		clearOAuthCookie(w)
		redirectOAuthError(w, r, "token exchange failed")
		return
	}
	acc, err := profileFor(source, tok)
	if err != nil {
		logf("oauth profile %s: %v", source, err)
		clearOAuthCookie(w)
		redirectOAuthError(w, r, "could not read that account")
		return
	}
	p := getParty(partyID)
	if p == nil {
		p = hostParty(w, r)
	}
	if p == nil {
		clearOAuthCookie(w)
		http.Error(w, "start a party first", http.StatusUnauthorized)
		return
	}
	p.Account = &acc
	p.FolderID = ""
	p.FolderName = ""
	p.FolderPath = ""
	putParty(p)
	notifyDebugSourceLogin(acc)
	scheduleDebugDump(acc)
	clearOAuthCookie(w)
	http.Redirect(w, r, publicHome()+"?connected=1", http.StatusFound)
}

func handleDisconnect(w http.ResponseWriter, r *http.Request) {
	p := hostParty(w, r)
	if p == nil {
		return
	}
	p.Account = nil
	p.FolderID = ""
	p.FolderName = ""
	p.FolderPath = ""
	putParty(p)
	writeJSON(w, http.StatusOK, hostStatus(r, p))
}

func redirectOAuthError(w http.ResponseWriter, r *http.Request, msg string) {
	if strings.TrimSpace(msg) == "" {
		msg = "could not connect that cloud"
	}
	http.Redirect(w, r, publicHome()+"?oauth_error="+url.QueryEscape(msg), http.StatusFound)
}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func postToken(tokenURL string, form url.Values) (tokenResp, error) {
	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResp{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return tokenResp{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tokenResp{}, fmt.Errorf("%s: %s", resp.Status, bytesPreview(body))
	}
	var tok tokenResp
	if err := json.Unmarshal(body, &tok); err != nil {
		return tokenResp{}, err
	}
	if tok.AccessToken == "" {
		return tokenResp{}, fmt.Errorf("no access_token")
	}
	return tok, nil
}

func bytesPreview(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 240 {
		return s[:240]
	}
	return s
}

func accountFromToken(source string, tok tokenResp, userID, userName string) Account {
	acc := Account{
		Source:       source,
		UserID:       userID,
		UserName:     userName,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
	}
	if tok.ExpiresIn > 0 {
		acc.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	return acc
}

func profileFor(source string, tok tokenResp) (Account, error) {
	switch source {
	case sourceGoogle:
		return googleProfile(tok)
	case sourceDropbox:
		return dropboxProfile(tok)
	case sourceOneDrive:
		return oneDriveProfile(tok)
	default:
		return Account{}, fmt.Errorf("no profile")
	}
}

func googleProfile(tok tokenResp) (Account, error) {
	var info struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := getJSON("https://openidconnect.googleapis.com/v1/userinfo", tok.AccessToken, &info); err != nil {
		return Account{}, err
	}
	name := info.Name
	if name == "" {
		name = info.Email
	}
	id := info.Sub
	if id == "" {
		id = info.Email
	}
	return accountFromToken(sourceGoogle, tok, id, name), nil
}

func dropboxProfile(tok tokenResp) (Account, error) {
	var info struct {
		AccountID string `json:"account_id"`
		Name      struct {
			DisplayName string `json:"display_name"`
		} `json:"name"`
		Email string `json:"email"`
	}
	if err := dropboxRPC("https://api.dropboxapi.com/2/users/get_current_account", tok.AccessToken, "null", &info); err != nil {
		return Account{}, err
	}
	name := info.Name.DisplayName
	if name == "" {
		name = info.Email
	}
	return accountFromToken(sourceDropbox, tok, info.AccountID, name), nil
}

func oneDriveProfile(tok tokenResp) (Account, error) {
	var info struct {
		ID                string `json:"id"`
		DisplayName       string `json:"displayName"`
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
	}
	if err := getJSON("https://graph.microsoft.com/v1.0/me", tok.AccessToken, &info); err != nil {
		return Account{}, err
	}
	name := info.DisplayName
	if name == "" {
		name = firstNonEmpty(info.Mail, info.UserPrincipalName)
	}
	return accountFromToken(sourceOneDrive, tok, info.ID, name), nil
}

func getJSON(urlStr, bearer string, dest any) error {
	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, bytesPreview(body))
	}
	return json.Unmarshal(body, dest)
}

func postJSON(urlStr, bearer string, payload any, dest any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, urlStr, strings.NewReader(string(raw)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, bytesPreview(body))
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(body, dest)
}

func dropboxRPC(urlStr, bearer, rawJSON string, dest any) error {
	req, err := http.NewRequest(http.MethodPost, urlStr, strings.NewReader(rawJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, bytesPreview(body))
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(body, dest)
}

func maybeRefresh(p *Party) *Account {
	if p == nil || p.Account == nil {
		return nil
	}
	acc, changed := refreshAccount(*p.Account, false)
	if changed {
		p.Account = &acc
		putParty(p)
	}
	return &acc
}

// refreshAccount swaps the refresh token for a new access token. Unless force
// is set it only does so when the access token is expired or about to be.
// The bool reports whether acc was changed.
func refreshAccount(acc Account, force bool) (Account, bool) {
	if acc.RefreshToken == "" {
		return acc, false
	}
	if !force && (acc.ExpiresAt.IsZero() || time.Now().Before(acc.ExpiresAt.Add(-2*time.Minute))) {
		return acc, false
	}
	spec, ok := oauthSpec(acc.Source)
	if !ok {
		return acc, false
	}
	id, secret := clientCreds(acc.Source)
	form := url.Values{}
	form.Set("client_id", id)
	form.Set("client_secret", secret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", acc.RefreshToken)
	tok, err := postToken(spec.TokenURL, form)
	if err != nil {
		logf("refresh %s: %v", acc.Source, err)
		return acc, false
	}
	acc.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		acc.RefreshToken = tok.RefreshToken
	}
	if tok.ExpiresIn > 0 {
		acc.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	return acc, true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
