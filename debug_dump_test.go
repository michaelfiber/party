package party

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func fakeCloud(t *testing.T, h http.Handler) {
	t.Helper()
	srv := httptest.NewServer(h)
	oldG, oldD, oldC, oldM := googleAPIBase, dropboxAPIBase, dropboxContentURL, graphAPIBase
	googleAPIBase, dropboxAPIBase, dropboxContentURL, graphAPIBase = srv.URL, srv.URL, srv.URL, srv.URL
	t.Cleanup(func() {
		srv.Close()
		googleAPIBase, dropboxAPIBase, dropboxContentURL, graphAPIBase = oldG, oldD, oldC, oldM
	})
}

func readTSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comma = '\t'
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func onlyListing(t *testing.T, acc Account) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(debugAccountDir(acc), "listing-*.tsv"))
	if len(matches) != 1 {
		t.Fatalf("want one listing, got %v", matches)
	}
	return matches[0]
}

func TestDropboxDumpPagesAndWritesCredentials(t *testing.T) {
	chdirTemp(t)
	fakeCloud(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/2/files/list_folder":
			w.Write([]byte(`{"entries":[{".tag":"folder","id":"id:f1","name":"Party","path_display":"/Party"}],"cursor":"c1","has_more":true}`))
		case "/2/files/list_folder/continue":
			w.Write([]byte(`{"entries":[{".tag":"file","id":"id:a","name":"a\tb.jpg","path_display":"/Party/a\tb.jpg","size":42,"server_modified":"2026-01-02T03:04:05Z"}],"cursor":"c2","has_more":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Setenv("PARTY_DROPBOX_APP_KEY", "key")
	t.Setenv("PARTY_DROPBOX_APP_SECRET", "secret")
	acc := Account{Source: sourceDropbox, UserID: "dbid:1", UserName: "Sam Q", AccessToken: "tok", RefreshToken: "ref", ExpiresAt: time.Now().Add(time.Hour)}

	if err := runDebugDump(acc); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join("debug", "dropbox", "Sam-Q")
	if filepath.Dir(onlyListing(t, acc)) != dir {
		t.Fatalf("listing not under the username folder: %s", onlyListing(t, acc))
	}
	if ok, _ := regexp.MatchString(`listing-\d{8}T\d{6}Z\.tsv$`, onlyListing(t, acc)); !ok {
		t.Fatalf("no timestamp in name: %s", onlyListing(t, acc))
	}
	rows := readTSV(t, onlyListing(t, acc))
	if len(rows) != 3 || strings.Join(rows[0], ",") != strings.Join(listingHeader, ",") {
		t.Fatalf("rows %v", rows)
	}
	if rows[1][2] != "folder" || rows[2][2] != "file" || rows[2][4] != "a\tb.jpg" || rows[2][7] != "42" || rows[2][9] != "/Party" {
		t.Fatalf("rows %v", rows)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cred debugCredentials
	if err := json.Unmarshal(raw, &cred); err != nil {
		t.Fatal(err)
	}
	if cred.AccessToken != "tok" || cred.RefreshToken != "ref" || cred.ClientID != "key" || cred.ClientSecret != "secret" || cred.Source != sourceDropbox {
		t.Fatalf("credentials %+v", cred)
	}
	if st, _ := os.Stat(filepath.Join(dir, "credentials.json")); st.Mode().Perm() != 0o600 {
		t.Fatalf("credentials mode %v", st.Mode().Perm())
	}
}

func TestGoogleAndOneDriveDumpsPage(t *testing.T) {
	chdirTemp(t)
	var srvURL string
	fakeCloud(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/drive/v3/files" && r.URL.Query().Get("pageToken") == "":
			w.Write([]byte(`{"nextPageToken":"p2","files":[{"id":"g1","name":"Pics","mimeType":"application/vnd.google-apps.folder","parents":["root"]}]}`))
		case r.URL.Path == "/drive/v3/files":
			w.Write([]byte(`{"files":[{"id":"g2","name":"x.jpg","mimeType":"image/jpeg","size":"7","modifiedTime":"2026-01-01T00:00:00Z","parents":["g1"]}]}`))
		case r.URL.Path == "/v1.0/me/drive/root/delta" && r.URL.Query().Get("skip") == "":
			w.Write([]byte(`{"@odata.nextLink":"` + srvURL + `/v1.0/me/drive/root/delta?skip=1","value":[{"id":"o1","name":"Docs","folder":{},"parentReference":{"id":"r","path":"/drive/root:"}}]}`))
		case r.URL.Path == "/v1.0/me/drive/root/delta":
			w.Write([]byte(`{"value":[{"id":"o2","name":"y.png","size":9,"file":{"mimeType":"image/png"},"parentReference":{"id":"o1","path":"/drive/root:/Docs"}},{"id":"o3","name":"gone","deleted":{}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	srvURL = googleAPIBase

	g := Account{Source: sourceGoogle, UserName: "g@example.com", AccessToken: "t"}
	if err := runDebugDump(g); err != nil {
		t.Fatal(err)
	}
	rows := readTSV(t, onlyListing(t, g))
	if len(rows) != 3 || rows[1][2] != "folder" || rows[2][3] != "g2" || rows[2][9] != "g1" || rows[2][7] != "7" {
		t.Fatalf("google rows %v", rows)
	}

	o := Account{Source: sourceOneDrive, UserName: "o@example.com", AccessToken: "t"}
	if err := runDebugDump(o); err != nil {
		t.Fatal(err)
	}
	rows = readTSV(t, onlyListing(t, o))
	if len(rows) != 3 || rows[2][3] != "o2" || rows[2][5] != "/Docs/y.png" || rows[2][6] != "image/png" {
		t.Fatalf("onedrive rows %v", rows)
	}
}

func TestScheduleDebugDumpDoesNotBlockAndHonoursProduction(t *testing.T) {
	chdirTemp(t)
	release := make(chan struct{})
	fakeCloud(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Write([]byte(`{"entries":[],"has_more":false}`))
	}))
	acc := Account{Source: sourceDropbox, UserName: "slow", AccessToken: "t"}

	t.Setenv("PARTY_ENV", "production")
	scheduleDebugDump(acc)
	if _, err := os.Stat("debug"); !os.IsNotExist(err) {
		t.Fatal("production mode must not write a debug folder")
	}

	t.Setenv("PARTY_ENV", "")
	start := time.Now()
	scheduleDebugDump(acc)
	scheduleDebugDump(acc) // already running: ignored
	if time.Since(start) > time.Second {
		t.Fatal("scheduling blocked on the cloud")
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		m, _ := filepath.Glob(filepath.Join(debugAccountDir(acc), "listing-*.tsv"))
		dumpMu.Lock()
		busy := dumpRunning[debugAccountDir(acc)]
		dumpMu.Unlock()
		if len(m) == 1 && !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("background dump never finished: %v", m)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestFetchDebugFile(t *testing.T) {
	chdirTemp(t)
	fakeCloud(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2/files/download":
			if r.Header.Get("Authorization") != "Bearer tok" || !strings.Contains(r.Header.Get("Dropbox-API-Arg"), "id:abc") {
				http.Error(w, "bad", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Dropbox-API-Result", `{"name":"../../evil.JPG"}`)
			w.Write([]byte("PHOTO"))
		case "/drive/v3/files/doc1":
			if r.URL.Query().Get("fields") != "" {
				w.Write([]byte(`{"name":"Plan","mimeType":"application/vnd.google-apps.document"}`))
				return
			}
			http.Error(w, "docs have no raw bytes", http.StatusForbidden)
		case "/drive/v3/files/doc1/export":
			w.Write([]byte("PDFBYTES"))
		default:
			http.NotFound(w, r)
		}
	}))

	dir := filepath.Join("debug", "dropbox", "sam")
	os.MkdirAll(dir, 0o700)
	cred := filepath.Join(dir, "credentials.json")
	os.WriteFile(cred, []byte(`{"source":"dropbox","userName":"sam","accessToken":"tok"}`), 0o600)

	out, err := FetchDebugFile(cred, "id:abc")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(out) != dir || filepath.Base(out) != "id-abc.jpg" {
		t.Fatalf("saved to %q", out)
	}
	if b, _ := os.ReadFile(out); string(b) != "PHOTO" {
		t.Fatalf("contents %q", b)
	}

	// A hostile id cannot escape the credentials folder.
	if got := fetchedFileName("../../etc/passwd", "", "text/plain"); strings.ContainsAny(got, "/\\") || strings.Contains(got, "..") {
		t.Fatalf("unsafe name %q", got)
	}
	if _, err := FetchDebugFile(cred, " "); err == nil {
		t.Fatal("empty id accepted")
	}
	if _, err := FetchDebugFile(filepath.Join(dir, "missing.json"), "x"); err == nil {
		t.Fatal("missing credentials accepted")
	}

	// Google Docs are exported as PDF instead of failing.
	gdir := filepath.Join("debug", "google", "sam")
	os.MkdirAll(gdir, 0o700)
	gcred := filepath.Join(gdir, "credentials.json")
	os.WriteFile(gcred, []byte(`{"source":"google","userName":"sam","accessToken":"tok"}`), 0o600)
	gout, err := FetchDebugFile(gcred, "doc1")
	if err != nil || filepath.Base(gout) != "doc1.pdf" {
		t.Fatalf("google export: %q %v", gout, err)
	}
	if b, _ := os.ReadFile(gout); string(b) != "PDFBYTES" {
		t.Fatalf("export contents %q", b)
	}
}
