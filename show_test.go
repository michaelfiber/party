package party

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSlideshowFeedFollowsShowKey(t *testing.T) {
	chdirTemp(t)
	created, auth := startTestParty(t, "Slides")
	var status struct {
		Party struct {
			Code    string `json:"code"`
			ShowURL string `json:"showUrl"`
		} `json:"party"`
	}
	if err := json.NewDecoder(created.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	i := strings.LastIndex(status.Party.ShowURL, "/show/")
	if i < 0 {
		t.Fatalf("show url %q", status.Party.ShowURL)
	}
	key := status.Party.ShowURL[i+len("/show/"):]
	if key == "" || key == status.Party.Code {
		t.Fatalf("show key %q must be set and differ from the guest code", key)
	}

	h := appHandler()
	if rec := postTestShot(t, h, status.Party.Code, nil); rec.Code != http.StatusOK {
		t.Fatalf("shot: %d %s", rec.Code, rec.Body.String())
	}

	feed := httptest.NewRecorder()
	h.ServeHTTP(feed, httptest.NewRequest(http.MethodGet, "/api/show/"+key, nil))
	if feed.Code != http.StatusOK {
		t.Fatalf("feed: %d %s", feed.Code, feed.Body.String())
	}
	var got struct {
		Name  string `json:"name"`
		Shots []struct {
			ID string `json:"id"`
		} `json:"shots"`
	}
	if err := json.NewDecoder(feed.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "Slides" || len(got.Shots) != 1 {
		t.Fatalf("feed %#v", got)
	}

	img := httptest.NewRecorder()
	h.ServeHTTP(img, httptest.NewRequest(http.MethodGet, "/api/show/"+key+"/shot/"+got.Shots[0].ID, nil))
	if img.Code != http.StatusOK || !bytes.HasPrefix(img.Body.Bytes(), []byte{0xff, 0xd8}) {
		t.Fatalf("shot file: %d", img.Code)
	}

	qr := httptest.NewRecorder()
	h.ServeHTTP(qr, httptest.NewRequest(http.MethodGet, "/api/show/"+key+"/qr", nil))
	if qr.Code != http.StatusOK || !bytes.HasPrefix(qr.Body.Bytes(), []byte{0x89, 0x50, 0x4e, 0x47}) {
		t.Fatalf("join qr: %d", qr.Code)
	}

	// The guest code must not open the slideshow.
	guest := httptest.NewRecorder()
	h.ServeHTTP(guest, httptest.NewRequest(http.MethodGet, "/api/show/"+status.Party.Code, nil))
	if guest.Code != http.StatusNotFound {
		t.Fatalf("guest code opened slideshow: %d", guest.Code)
	}

	// Only the host can rotate the link.
	anon := httptest.NewRecorder()
	h.ServeHTTP(anon, httptest.NewRequest(http.MethodPost, "/api/show/reset", nil))
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous reset: %d", anon.Code)
	}
	reset := httptest.NewRecorder()
	h.ServeHTTP(reset, requestWithCookies(http.MethodPost, "/api/show/reset", []byte("{}"), ownerCookies(created, auth)...))
	if reset.Code != http.StatusOK || strings.Contains(reset.Body.String(), key) {
		t.Fatalf("reset: %d", reset.Code)
	}
	old := httptest.NewRecorder()
	h.ServeHTTP(old, httptest.NewRequest(http.MethodGet, "/api/show/"+key, nil))
	if old.Code != http.StatusNotFound {
		t.Fatalf("old key still works: %d", old.Code)
	}
}
