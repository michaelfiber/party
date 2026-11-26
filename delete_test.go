package party

import (
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeletePartyRemovesPhotos(t *testing.T) {
	chdirTemp(t)
	created, auth := startTestParty(t, "Delete me")
	var status struct {
		Token string `json:"token"`
		Party struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"party"`
	}
	if err := json.NewDecoder(created.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	h := appHandler()
	if rec := postTestShot(t, h, status.Party.Code, nil); rec.Code != http.StatusOK {
		t.Fatalf("shot: %d %s", rec.Code, rec.Body.String())
	}
	dir := filepath.Join(photosDir, status.Party.ID)
	if entries, err := os.ReadDir(dir); err != nil || len(entries) == 0 {
		t.Fatalf("expected a photo on disk: %v", err)
	}

	// Someone else's phone cannot delete it.
	_, otherAuth := loginTestPhone(t, "5555550199")
	body := []byte(`{"id":"` + status.Party.ID + `"}`)
	other := httptest.NewRecorder()
	h.ServeHTTP(other, requestWithCookies(http.MethodPost, "/api/parties/delete", body, otherAuth))
	if other.Code != http.StatusNotFound {
		t.Fatalf("other phone delete: %d", other.Code)
	}
	if getParty(status.Party.ID) == nil {
		t.Fatal("party deleted by the wrong phone")
	}

	// Log back in as the host's phone and delete.
	_, auth = loginTestPhone(t, "5555550100")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, requestWithCookies(http.MethodPost, "/api/parties/delete", body, auth))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), status.Party.ID) {
		t.Fatal("deleted party still listed")
	}
	if getParty(status.Party.ID) != nil || getPartyByCode(status.Party.Code) != nil {
		t.Fatal("party still in the store")
	}
	if len(shotsForParty(status.Party.ID)) != 0 {
		t.Fatal("shot records left behind")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("photo folder still on disk: %v", err)
	}
	join := httptest.NewRecorder()
	h.ServeHTTP(join, httptest.NewRequest(http.MethodGet, "/api/join/"+status.Party.Code, nil))
	if join.Code != http.StatusNotFound {
		t.Fatalf("guest link still works: %d", join.Code)
	}

	// Survives a reload from disk.
	loadStore()
	if getParty(status.Party.ID) != nil {
		t.Fatal("party came back after reload")
	}
}

func TestLegalPagesLinkedFromHome(t *testing.T) {
	h := appHandler()
	home := httptest.NewRecorder()
	h.ServeHTTP(home, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(home.Body.String(), `href="terms"`) || !strings.Contains(home.Body.String(), `href="privacy"`) {
		t.Fatal("home footer missing legal links")
	}
	for path, want := range map[string]string{"/terms": "Terms of Service", "/privacy": "Privacy Policy"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<h1>"+want+"</h1>") {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}

func TestPrintPageAndLargeQR(t *testing.T) {
	h := appHandler()
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/print", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "they go straight to the host's cloud") {
		t.Fatalf("print page: %d", page.Code)
	}

	chdirTemp(t)
	created, auth := startTestParty(t, "Print me")
	qr := httptest.NewRecorder()
	h.ServeHTTP(qr, requestWithCookies(http.MethodGet, "/api/qr?size=900", nil, ownerCookies(created, auth)...))
	if qr.Code != http.StatusOK {
		t.Fatalf("qr: %d", qr.Code)
	}
	img, err := png.Decode(qr.Body)
	if err != nil || img.Bounds().Dx() != 900 {
		t.Fatalf("qr size: %v %v", err, img)
	}
	anon := httptest.NewRecorder()
	h.ServeHTTP(anon, httptest.NewRequest(http.MethodGet, "/api/qr?size=900", nil))
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous qr: %d", anon.Code)
	}
}
