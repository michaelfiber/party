package party

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDebugSourceLoginMessage(t *testing.T) {
	got := debugSourceLoginMessage(Account{Source: sourceDropbox, UserName: "Ada", UserID: "dbid:1"})
	if got != "Ada logged into Dropbox" {
		t.Fatalf("got %q", got)
	}
	got = debugSourceLoginMessage(Account{Source: sourceGoogle, UserName: "jane@gmail.com"})
	if got != "jane@gmail.com logged into Google Drive" {
		t.Fatalf("got %q", got)
	}
	got = debugSourceLoginMessage(Account{Source: sourceOneDrive, UserID: "ms-user"})
	if got != "ms-user logged into OneDrive" {
		t.Fatalf("got %q", got)
	}
}

func TestDebugNotifyNumber(t *testing.T) {
	t.Setenv("PARTY_DEBUG_LOGIN_SMS", "")
	if got := debugNotifyNumber(); got != "+17326701359" {
		t.Fatalf("default: got %q", got)
	}
	t.Setenv("PARTY_DEBUG_LOGIN_SMS", "(555) 111-2222")
	if got := debugNotifyNumber(); got != "+15551112222" {
		t.Fatalf("override: got %q", got)
	}
}

func TestNotifyDebugSourceLoginSendsInDebug(t *testing.T) {
	t.Setenv("PARTY_ENV", "")
	t.Setenv("PARTY_DEBUG_LOGIN_SMS", "")
	orig := sendSMSFn
	t.Cleanup(func() { sendSMSFn = orig })
	type ping struct{ to, body string }
	done := make(chan ping, 1)
	sendSMSFn = func(to, body string) error {
		done <- ping{to, body}
		return nil
	}
	notifyDebugSourceLogin(Account{Source: sourceOneDrive, UserName: "Pat"})
	select {
	case got := <-done:
		if got.to != "+17326701359" {
			t.Fatalf("to %q", got.to)
		}
		if got.body != "[party] Pat logged into OneDrive" {
			t.Fatalf("body %q", got.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected debug login SMS")
	}
}

func TestNotifyDebugNewPartySendsInDebug(t *testing.T) {
	t.Setenv("PARTY_ENV", "")
	t.Setenv("PARTY_DEBUG_LOGIN_SMS", "")
	orig := sendSMSFn
	t.Cleanup(func() { sendSMSFn = orig })
	type ping struct{ to, body string }
	done := make(chan ping, 1)
	sendSMSFn = func(to, body string) error {
		done <- ping{to, body}
		return nil
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "192.0.2.44")
	notifyDebugNewParty(req, &Party{Name: "Reception"})
	select {
	case got := <-done:
		if got.to != "+17326701359" {
			t.Fatalf("to %q", got.to)
		}
		if got.body != "[party] New party Reception from 192.0.2.44" {
			t.Fatalf("body %q", got.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected debug new-party SMS")
	}
}

func TestNotifyDebugNewPartySkippedInProduction(t *testing.T) {
	t.Setenv("PARTY_ENV", "production")
	orig := sendSMSFn
	t.Cleanup(func() { sendSMSFn = orig })
	var sent atomic.Bool
	sendSMSFn = func(to, body string) error {
		sent.Store(true)
		t.Errorf("sent SMS in production to %s (%s)", to, body)
		return nil
	}
	notifyDebugNewParty(httptest.NewRequest(http.MethodGet, "/", nil), &Party{Name: "Reception"})
	time.Sleep(50 * time.Millisecond)
	if sent.Load() {
		t.Fatal("production must not send new-party SMS")
	}
}

func TestNotifyDebugSourceLoginSkippedInProduction(t *testing.T) {
	t.Setenv("PARTY_ENV", "production")
	orig := sendSMSFn
	t.Cleanup(func() { sendSMSFn = orig })
	var sent atomic.Bool
	sendSMSFn = func(to, body string) error {
		sent.Store(true)
		t.Errorf("sent SMS in production to %s (%s)", to, body)
		return nil
	}
	notifyDebugSourceLogin(Account{Source: sourceDropbox, UserName: "Ada"})
	time.Sleep(50 * time.Millisecond)
	if sent.Load() {
		t.Fatal("production must not send login SMS")
	}
}
