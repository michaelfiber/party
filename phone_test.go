package party

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreatePartyRequiresLogin(t *testing.T) {
	chdirTemp(t)
	rec := httptest.NewRecorder()
	handleCreateParty(rec, httptest.NewRequest(http.MethodPost, "/api/party", strings.NewReader(`{"name":"Reception"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("create without login: %d %s", rec.Code, rec.Body.String())
	}
}

func TestLoginListsAndOpensParties(t *testing.T) {
	chdirTemp(t)
	first, _ := startTestParty(t, "Rehearsal dinner")
	var created struct {
		Party struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"party"`
		Parties []partyInfo `json:"parties"`
		Phone   string      `json:"phone"`
	}
	if err := json.NewDecoder(first.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Party.Name != "Rehearsal dinner" || created.Party.ID == "" {
		t.Fatalf("created %#v", created.Party)
	}
	if len(created.Parties) != 1 || created.Parties[0].ID != created.Party.ID {
		t.Fatalf("create should return this phone's parties, got %#v", created.Parties)
	}
	if !strings.Contains(created.Phone, "0100") {
		t.Fatalf("masked phone %q", created.Phone)
	}

	second, _ := startTestParty(t, "Sunday brunch")
	var listed struct {
		Parties []partyInfo `json:"parties"`
		Party   struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"party"`
	}
	if err := json.NewDecoder(second.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if listed.Party.Name != "Sunday brunch" {
		t.Fatalf("second party %#v", listed.Party)
	}
	if len(listed.Parties) != 2 {
		t.Fatalf("want 2 parties, got %#v", listed.Parties)
	}
	if listed.Parties[0].Name != "Sunday brunch" {
		t.Fatalf("newest party should be first, got %#v", listed.Parties)
	}

	token, auth := loginTestPhone(t, "5555550100")
	list := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/parties", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if auth != nil {
		req.AddCookie(auth)
	}
	handleListParties(list, req)
	if list.Code != http.StatusOK {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	var home struct {
		NeedsLogin bool        `json:"needsLogin"`
		Parties    []partyInfo `json:"parties"`
	}
	if err := json.NewDecoder(list.Body).Decode(&home); err != nil {
		t.Fatal(err)
	}
	if home.NeedsLogin || len(home.Parties) != 2 {
		t.Fatalf("home %#v", home)
	}

	open := httptest.NewRecorder()
	openBody, _ := json.Marshal(map[string]string{"id": created.Party.ID})
	openReq := httptest.NewRequest(http.MethodPost, "/api/parties/open", bytes.NewReader(openBody))
	openReq.Header.Set("Content-Type", "application/json")
	openReq.Header.Set("Authorization", "Bearer "+token)
	if auth != nil {
		openReq.AddCookie(auth)
	}
	handleOpenParty(open, openReq)
	if open.Code != http.StatusOK {
		t.Fatalf("open: %d %s", open.Code, open.Body.String())
	}
	if got := cookieByName(open, hostCookieName); got == nil || got.Value != created.Party.ID {
		t.Fatalf("open should set host cookie to %s, got %#v", created.Party.ID, got)
	}
	if cookieByName(open, secretCookieName) == nil {
		t.Fatal("open should set secret cookie")
	}
	if !strings.Contains(open.Body.String(), `"name":"Rehearsal dinner"`) {
		t.Fatalf("open body %s", open.Body.String())
	}

	otherToken, otherAuth := loginTestPhone(t, "5555550199")
	denied := httptest.NewRecorder()
	deniedReq := httptest.NewRequest(http.MethodPost, "/api/parties/open", bytes.NewReader(openBody))
	deniedReq.Header.Set("Content-Type", "application/json")
	deniedReq.Header.Set("Authorization", "Bearer "+otherToken)
	if otherAuth != nil {
		deniedReq.AddCookie(otherAuth)
	}
	handleOpenParty(denied, deniedReq)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("other phone open: %d %s", denied.Code, denied.Body.String())
	}

	loadStore()
	refresh := httptest.NewRecorder()
	again := httptest.NewRequest(http.MethodGet, "/api/parties", nil)
	again.Header.Set("Authorization", "Bearer "+token)
	handleListParties(refresh, again)
	if refresh.Code != http.StatusOK || !strings.Contains(refresh.Body.String(), "Rehearsal dinner") {
		t.Fatalf("token after reload: %d %s", refresh.Code, refresh.Body.String())
	}

	out := httptest.NewRecorder()
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/phone/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+token)
	handlePhoneLogout(out, logoutReq)
	if out.Code != http.StatusOK {
		t.Fatalf("logout: %d %s", out.Code, out.Body.String())
	}
	loggedOut := httptest.NewRecorder()
	gone := httptest.NewRequest(http.MethodGet, "/api/parties", nil)
	gone.Header.Set("Authorization", "Bearer "+token)
	handleListParties(loggedOut, gone)
	if loggedOut.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out token should not list parties, got %d %s", loggedOut.Code, loggedOut.Body.String())
	}
}

func TestWrongSMSCodeIsRejected(t *testing.T) {
	chdirTemp(t)
	allowPhoneSend("5555550100")
	orig := sendSMSFn
	sendSMSFn = func(string, string) error { return nil }
	t.Cleanup(func() { sendSMSFn = orig })

	send := httptest.NewRecorder()
	handlePhoneSend(send, httptest.NewRequest(http.MethodPost, "/api/phone/send", strings.NewReader(`{"phone":"5555550100"}`)))
	if send.Code != http.StatusOK {
		t.Fatalf("send: %d %s", send.Code, send.Body.String())
	}

	bad := httptest.NewRecorder()
	handlePhoneVerify(bad, httptest.NewRequest(http.MethodPost, "/api/phone/verify", strings.NewReader(`{"phone":"5555550100","code":"000000"}`)))
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code: %d %s", bad.Code, bad.Body.String())
	}
}

func TestPhoneSendRequiresTwilio(t *testing.T) {
	chdirTemp(t)
	t.Setenv("PARTY_ENV", "production")
	orig := sendSMSFn
	t.Cleanup(func() { sendSMSFn = orig })
	sendSMSFn = func(string, string) error {
		return errTwilioMissing
	}
	rec := httptest.NewRecorder()
	handlePhoneSend(rec, httptest.NewRequest(http.MethodPost, "/api/phone/send", strings.NewReader(`{"phone":"7325550199"}`)))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}

func TestDebugSendReturnsCodeWhenTwilioMissing(t *testing.T) {
	chdirTemp(t)
	t.Setenv("PARTY_ENV", "")
	orig := sendSMSFn
	t.Cleanup(func() { sendSMSFn = orig })
	sendSMSFn = func(string, string) error {
		return errTwilioMissing
	}
	rec := httptest.NewRecorder()
	handlePhoneSend(rec, httptest.NewRequest(http.MethodPost, "/api/phone/send", strings.NewReader(`{"phone":"7325550199"}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"debugCode"`) {
		t.Fatalf("debug send: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMaskPhone(t *testing.T) {
	got := maskPhone("+17325550100")
	if !strings.HasPrefix(got, "+1") || !strings.HasSuffix(got, "0100") || !strings.Contains(got, "•") {
		t.Fatalf("got %q", got)
	}
}

var errTwilioMissing = errString("TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN, and TWILIO_FROM_NUMBER are required")

type errString string

func (e errString) Error() string { return string(e) }
