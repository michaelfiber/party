package party

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	sendSMSFn = func(string, string) error { return nil }
	os.Exit(m.Run())
}

func chdirTemp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	loadStore()
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 12, 12))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func cookieByName(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func requestWithCookies(method, target string, body []byte, cookies ...*http.Cookie) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, target, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	for _, c := range cookies {
		if c != nil {
			r.AddCookie(c)
		}
	}
	return r
}

func allowPhoneSend(phone string) {
	n, err := normalizePhone(phone)
	if err != nil {
		n = phone
	}
	phoneMu.Lock()
	delete(lastSend, n)
	phoneMu.Unlock()
}

func extractSMSCode(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`\b(\d{6})\b`).FindStringSubmatch(body)
	if len(m) < 2 {
		t.Fatalf("no code in %q", body)
	}
	return m[1]
}

func loginTestPhone(t *testing.T, phone string) (token string, auth *http.Cookie) {
	t.Helper()
	allowPhoneSend(phone)
	orig := sendSMSFn
	var smsBody string
	sendSMSFn = func(to, body string) error {
		smsBody = body
		if orig != nil {
			return orig(to, body)
		}
		return nil
	}
	sendRec := httptest.NewRecorder()
	sendBody, _ := json.Marshal(map[string]string{"phone": phone})
	handlePhoneSend(sendRec, httptest.NewRequest(http.MethodPost, "/api/phone/send", bytes.NewReader(sendBody)))
	sendSMSFn = orig
	if sendRec.Code != http.StatusOK {
		t.Fatalf("send code: %d %s", sendRec.Code, sendRec.Body.String())
	}
	code := extractSMSCode(t, smsBody)

	verifyRec := httptest.NewRecorder()
	verifyBody, _ := json.Marshal(map[string]string{"phone": phone, "code": code})
	handlePhoneVerify(verifyRec, httptest.NewRequest(http.MethodPost, "/api/phone/verify", bytes.NewReader(verifyBody)))
	if verifyRec.Code != http.StatusOK {
		t.Fatalf("verify code: %d %s", verifyRec.Code, verifyRec.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(verifyRec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Token == "" {
		t.Fatal("verify should return a token")
	}
	return out.Token, cookieByName(verifyRec, authCookieName)
}

func ownerCookies(rec *httptest.ResponseRecorder, auth *http.Cookie) []*http.Cookie {
	return []*http.Cookie{
		cookieByName(rec, hostCookieName),
		cookieByName(rec, secretCookieName),
		auth,
	}
}

func startTestParty(t *testing.T, name string) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	token, auth := loginTestPhone(t, "5555550100")
	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"name": name})
	req := httptest.NewRequest(http.MethodPost, "/api/party", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if auth != nil {
		req.AddCookie(auth)
	}
	handleCreateParty(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create party: %d %s", rec.Code, rec.Body.String())
	}
	if cookieByName(rec, hostCookieName) == nil || cookieByName(rec, secretCookieName) == nil {
		t.Fatal("create party missing cookies")
	}
	if auth == nil {
		t.Fatal("login missing auth cookie")
	}
	return rec, auth
}

func TestStartPartyAndGuestShot(t *testing.T) {
	chdirTemp(t)
	created, auth := startTestParty(t, "Sam & Alex")
	var status struct {
		Party struct {
			Code       string `json:"code"`
			Name       string `json:"name"`
			JoinURL    string `json:"joinUrl"`
			PhotoCount int    `json:"photoCount"`
		} `json:"party"`
	}
	if err := json.NewDecoder(created.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Party.Name != "Sam & Alex" || status.Party.Code == "" || !strings.Contains(status.Party.JoinURL, "/p/"+status.Party.Code) {
		t.Fatalf("party %#v", status.Party)
	}

	idle := httptest.NewRecorder()
	handleStatus(idle, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if idle.Code != http.StatusOK || !strings.Contains(idle.Body.String(), `"needsLogin":true`) {
		t.Fatalf("anonymous status: %d %s", idle.Code, idle.Body.String())
	}

	host := httptest.NewRecorder()
	handleStatus(host, requestWithCookies(http.MethodGet, "/api/status", nil, ownerCookies(created, auth)...))
	if host.Code != http.StatusOK || strings.Contains(host.Body.String(), `"needsLogin":true`) {
		t.Fatalf("host status: %d %s", host.Code, host.Body.String())
	}

	qr := httptest.NewRecorder()
	handleShareQR(qr, requestWithCookies(http.MethodGet, "/api/qr", nil, ownerCookies(created, auth)...))
	if qr.Code != http.StatusOK || !bytes.HasPrefix(qr.Body.Bytes(), []byte{0x89, 0x50, 0x4e, 0x47}) {
		t.Fatalf("qr: %d", qr.Code)
	}

	h := appHandler()
	join := httptest.NewRecorder()
	h.ServeHTTP(join, httptest.NewRequest(http.MethodGet, "/api/join/"+status.Party.Code, nil))
	var joined struct {
		OK   bool   `json:"ok"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(join.Body).Decode(&joined); err != nil {
		t.Fatal(err)
	}
	if join.Code != http.StatusOK || !joined.OK || joined.Name != "Sam & Alex" {
		t.Fatalf("join: %d %#v", join.Code, joined)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("photo", "shot.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(jpegBytes(t)); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("facing", "user"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	shotReq := httptest.NewRequest(http.MethodPost, "/api/join/"+status.Party.Code+"/shot", &body)
	shotReq.Header.Set("Content-Type", mw.FormDataContentType())
	shot := httptest.NewRecorder()
	h.ServeHTTP(shot, shotReq)
	if shot.Code != http.StatusOK {
		t.Fatalf("shot: %d %s", shot.Code, shot.Body.String())
	}
	var shotBody struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
	}
	if err := json.NewDecoder(shot.Body).Decode(&shotBody); err != nil {
		t.Fatal(err)
	}
	if !shotBody.OK || shotBody.Count != 1 {
		t.Fatalf("shot body %#v", shotBody)
	}

	missing := httptest.NewRecorder()
	h.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/join/notaparty", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing join: %d", missing.Code)
	}

	denied := httptest.NewRecorder()
	handleListShots(denied, httptest.NewRequest(http.MethodGet, "/api/shots", nil))
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("shots without host: %d", denied.Code)
	}

	hostOnly := httptest.NewRecorder()
	handleListShots(hostOnly, requestWithCookies(http.MethodGet, "/api/shots", nil, cookieByName(created, hostCookieName), cookieByName(created, secretCookieName)))
	if hostOnly.Code != http.StatusUnauthorized {
		t.Fatalf("shots with host cookies but no phone: %d %s", hostOnly.Code, hostOnly.Body.String())
	}

	list := httptest.NewRecorder()
	handleListShots(list, requestWithCookies(http.MethodGet, "/api/shots", nil, ownerCookies(created, auth)...))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"count":1`) {
		t.Fatalf("list shots: %d %s", list.Code, list.Body.String())
	}

	raw, err := os.ReadFile(partiesFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"photoCount": 1`) || !strings.Contains(string(raw), "Sam") {
		t.Fatalf("parties.json missing party: %s", raw)
	}
}

func postTestShot(t *testing.T, h http.Handler, code string, extra map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("photo", "shot.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(jpegBytes(t)); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("facing", "user"); err != nil {
		t.Fatal(err)
	}
	for k, v := range extra {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/join/"+code+"/shot", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestDebugShotSavesToDebugFolder(t *testing.T) {
	chdirTemp(t)
	t.Setenv("PARTY_ENV", "")
	created, _ := startTestParty(t, "Reception")
	var status struct {
		Party struct {
			Code string `json:"code"`
			ID   string `json:"id"`
		} `json:"party"`
	}
	if err := json.NewDecoder(created.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	h := appHandler()
	join := httptest.NewRecorder()
	h.ServeHTTP(join, httptest.NewRequest(http.MethodGet, "/api/join/"+status.Party.Code, nil))
	if !strings.Contains(join.Body.String(), `"debug":true`) {
		t.Fatalf("join should advertise debug mode: %s", join.Body.String())
	}

	shot := postTestShot(t, h, status.Party.Code, map[string]string{"debug": "1"})
	if shot.Code != http.StatusOK {
		t.Fatalf("debug shot: %d %s", shot.Code, shot.Body.String())
	}
	var body struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
		Debug bool `json:"debug"`
	}
	if err := json.NewDecoder(shot.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || !body.Debug || body.Count != 0 {
		t.Fatalf("debug shot should not count, got %#v", body)
	}
	entries, err := os.ReadDir(filepath.Join(photosDir, status.Party.ID, "debug"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("debug folder: %v %#v", err, entries)
	}

	storeMu.Lock()
	p := parties[status.Party.ID]
	if p == nil {
		storeMu.Unlock()
		t.Fatal("missing party")
	}
	acc := Account{Source: sourceDropbox, AccessToken: "tok", UserID: "dbid"}
	p.Account = &acc
	p.FolderID = "album"
	p.FolderPath = "/Reception"
	p.DebugFolderID = ""
	p.DebugFolderPath = ""
	storeMu.Unlock()
	putParty(p)

	clouded := postTestShot(t, h, status.Party.Code, map[string]string{"debug": "1"})
	if clouded.Code != http.StatusOK {
		t.Fatalf("debug shot with cloud: %d %s", clouded.Code, clouded.Body.String())
	}
	if strings.Contains(clouded.Body.String(), `"cloud":true`) {
		t.Fatalf("debug shot must not upload to cloud: %s", clouded.Body.String())
	}
	again := getParty(status.Party.ID)
	if again == nil || again.DebugFolderID != "" || again.DebugFolderPath != "" {
		t.Fatalf("must not create a cloud debug folder, party %#v", again)
	}

	if err := uploadShotToCloud(again, &Shot{Name: "debug-still.jpg", Debug: true}, jpegBytes(t)); err == nil {
		t.Fatal("uploadShotToCloud must refuse debug shots")
	}

	t.Setenv("PARTY_ENV", "production")
	denied := postTestShot(t, h, status.Party.Code, map[string]string{"debug": "1"})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("production debug shot: %d %s", denied.Code, denied.Body.String())
	}
}

func TestGuestCannotUseHostRoutes(t *testing.T) {
	chdirTemp(t)
	_, _ = startTestParty(t, "Kitchen")
	rec := httptest.NewRecorder()
	handleListFolders(rec, httptest.NewRequest(http.MethodGet, "/api/folders", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("folders without host: %d", rec.Code)
	}
}

func TestIndexMentionsPartyCam(t *testing.T) {
	h := appHandler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("index %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Text me a code") || !strings.Contains(body, "Start a party") || !strings.Contains(body, "Party Cam") || !strings.Contains(body, "id=\"qr\"") || !strings.Contains(body, "banner.js") {
		t.Fatalf("index missing host UI: %s", body[:min(400, len(body))])
	}
}

func TestHostedAtCubeofbParty(t *testing.T) {
	chdirTemp(t)
	t.Setenv("PARTY_PUBLIC_BASE_URL", "https://cubeofb.org/party")

	if publicPath() != "/party" {
		t.Fatalf("public path %q", publicPath())
	}

	token, auth := loginTestPhone(t, "5555550100")
	req := httptest.NewRequest(http.MethodPost, "/api/party", strings.NewReader(`{"name":"Reception"}`))
	req.Host = "cubeofb.org"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if auth != nil {
		req.AddCookie(auth)
	}
	rec := httptest.NewRecorder()
	handleCreateParty(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var status struct {
		Party struct {
			Code    string `json:"code"`
			JoinURL string `json:"joinUrl"`
		} `json:"party"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	wantJoin := "https://cubeofb.org/party/p/" + status.Party.Code
	if status.Party.JoinURL != wantJoin {
		t.Fatalf("join url %q want %q", status.Party.JoinURL, wantJoin)
	}
	if oauthRedirectURL(req, "google") != "https://cubeofb.org/party/api/oauth/google/callback" {
		t.Fatalf("oauth redirect %s", oauthRedirectURL(req, "google"))
	}
	idCookie := cookieByName(rec, hostCookieName)
	if idCookie == nil || idCookie.Path != "/party" {
		t.Fatalf("host cookie path %#v", idCookie)
	}

	h := appHandler()
	st := httptest.NewRecorder()
	h.ServeHTTP(st, httptest.NewRequest(http.MethodGet, "/party/api/status", nil))
	if st.Code != http.StatusOK || !strings.Contains(st.Body.String(), `"needsLogin":true`) {
		t.Fatalf("hosted status: %d %s", st.Code, st.Body.String())
	}

	t.Setenv("PARTY_PUBLIC_BASE_URL", "")
	cube := httptest.NewRequest(http.MethodGet, "/", nil)
	cube.Host = "cubeofb.org"
	if publicBaseURL(cube) != hostedBaseURL {
		t.Fatalf("cube host should still publish under /party, got %s", publicBaseURL(cube))
	}
}

func TestNormalizePhone(t *testing.T) {
	got, err := normalizePhone("(555) 555-0100")
	if err != nil || got != "+15555550100" {
		t.Fatalf("got %q %v", got, err)
	}
}
