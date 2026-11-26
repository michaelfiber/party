package party

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Debug cloud dump.
//
// When PARTY_ENV is not "production", connecting a cloud starts a background
// task that writes, under debug/<source>/<username>/:
//
//	credentials.json            the tokens, so cmd/getfile can use them
//	listing-<timestamp>.tsv     every file and folder on that cloud
//
// Nothing here ever blocks the OAuth redirect, and nothing is sent anywhere.

const (
	debugDir = "debug"

	// A runaway listing (or a loop in a provider's paging) must not run forever.
	maxListingPages = 5000
)

// Provider endpoints. Variables so tests can point them at a fake server.
var (
	googleAPIBase     = "https://www.googleapis.com"
	dropboxAPIBase    = "https://api.dropboxapi.com"
	dropboxContentURL = "https://content.dropboxapi.com"
	graphAPIBase      = "https://graph.microsoft.com"
)

var (
	dumpMu      sync.Mutex
	dumpRunning = map[string]bool{}
)

var listingHeader = []string{
	"source", "user", "item_type", "id", "name", "path", "mime", "size", "modified", "parent_id",
}

// debugCredentials is the credentials.json file. It carries the OAuth client
// id and secret too, so a refresh works without this server's environment.
type debugCredentials struct {
	Source       string `json:"source"`
	UserID       string `json:"userId"`
	UserName     string `json:"userName,omitempty"`
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
}

type listingRow struct {
	Type, ID, Name, Path, Mime, Modified, Parent string
	Size                                         int64
}

type listingSink func(listingRow) error

func safePathPart(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == '@', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if len(out) > 80 {
		out = out[:80]
	}
	if out == "" {
		return "unknown"
	}
	return out
}

func debugUsername(acc Account) string {
	for _, v := range []string{acc.UserName, acc.UserID} {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "unknown"
}

func debugAccountDir(acc Account) string {
	return filepath.Join(debugDir, safePathPart(acc.Source), safePathPart(debugUsername(acc)))
}

func debugCredentialsPath(acc Account) string {
	return filepath.Join(debugAccountDir(acc), "credentials.json")
}

func debugListingPath(acc Account, at time.Time) string {
	return filepath.Join(debugAccountDir(acc), "listing-"+at.UTC().Format("20060102T150405Z")+".tsv")
}

// scheduleDebugDump returns immediately; the work happens in a goroutine.
func scheduleDebugDump(acc Account) {
	if !debugMode() || acc.Source == "" || acc.AccessToken == "" {
		return
	}
	key := debugAccountDir(acc)
	dumpMu.Lock()
	if dumpRunning[key] {
		dumpMu.Unlock()
		return
	}
	dumpRunning[key] = true
	dumpMu.Unlock()

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				logf("debug dump %s: panic %v", key, rec)
			}
			dumpMu.Lock()
			delete(dumpRunning, key)
			dumpMu.Unlock()
		}()
		if err := runDebugDump(acc); err != nil {
			logf("debug dump %s: %v", key, err)
		}
	}()
}

func runDebugDump(acc Account) error {
	if err := writeDebugCredentials(acc); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	path := debugListingPath(acc, time.Now())
	n, err := writeListing(path, acc)
	if err != nil {
		return err
	}
	logf("debug listing %s (%d items)", path, n)
	return nil
}

func writeDebugCredentials(acc Account) error {
	id, secret := clientCreds(acc.Source)
	cred := debugCredentials{
		Source:       acc.Source,
		UserID:       acc.UserID,
		UserName:     acc.UserName,
		AccessToken:  acc.AccessToken,
		RefreshToken: acc.RefreshToken,
		ClientID:     id,
		ClientSecret: secret,
	}
	if !acc.ExpiresAt.IsZero() {
		cred.ExpiresAt = acc.ExpiresAt.UTC().Format(time.RFC3339)
	}
	path := debugCredentialsPath(acc)
	// Tokens and the client secret live here: owner-only, like data/*.json.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// writeListing walks the whole cloud and writes one TSV row per file/folder.
// A partial file is kept if the walk fails midway, so the error is not lost
// along with everything already fetched.
func writeListing(path string, acc Account) (int, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	cw.Comma = '\t'
	if err := cw.Write(listingHeader); err != nil {
		return 0, err
	}
	user := debugUsername(acc)
	count := 0
	sink := func(r listingRow) error {
		count++
		return cw.Write([]string{
			acc.Source, user, r.Type, r.ID, r.Name, r.Path, r.Mime,
			strconv.FormatInt(r.Size, 10), r.Modified, r.Parent,
		})
	}
	var walkErr error
	switch acc.Source {
	case sourceGoogle:
		walkErr = walkGoogle(acc, sink)
	case sourceDropbox:
		walkErr = walkDropbox(acc, sink)
	case sourceOneDrive:
		walkErr = walkOneDrive(acc, sink)
	default:
		walkErr = fmt.Errorf("unknown cloud %q", acc.Source)
	}
	cw.Flush()
	if err := cw.Error(); err != nil && walkErr == nil {
		walkErr = err
	}
	return count, walkErr
}

func walkGoogle(acc Account, emit listingSink) error {
	const folderMime = "application/vnd.google-apps.folder"
	token := ""
	for page := 0; page < maxListingPages; page++ {
		q := url.Values{}
		q.Set("pageSize", "1000")
		q.Set("q", "trashed=false")
		q.Set("fields", "nextPageToken,files(id,name,mimeType,size,modifiedTime,parents)")
		if token != "" {
			q.Set("pageToken", token)
		}
		var payload struct {
			NextPageToken string `json:"nextPageToken"`
			Files         []struct {
				ID       string   `json:"id"`
				Name     string   `json:"name"`
				MimeType string   `json:"mimeType"`
				Size     string   `json:"size"`
				Modified string   `json:"modifiedTime"`
				Parents  []string `json:"parents"`
			} `json:"files"`
		}
		if err := getJSON(googleAPIBase+"/drive/v3/files?"+q.Encode(), acc.AccessToken, &payload); err != nil {
			return fmt.Errorf("google list: %w", err)
		}
		for _, f := range payload.Files {
			size, _ := strconv.ParseInt(f.Size, 10, 64)
			kind := "file"
			if f.MimeType == folderMime {
				kind = "folder"
			}
			parent := ""
			if len(f.Parents) > 0 {
				parent = f.Parents[0]
			}
			if err := emit(listingRow{Type: kind, ID: f.ID, Name: f.Name, Mime: f.MimeType, Size: size, Modified: f.Modified, Parent: parent}); err != nil {
				return err
			}
		}
		if payload.NextPageToken == "" {
			return nil
		}
		token = payload.NextPageToken
	}
	return fmt.Errorf("google list: stopped after %d pages", maxListingPages)
}

type dropboxEntry struct {
	Tag      string `json:".tag"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path_display"`
	Size     int64  `json:"size"`
	Modified string `json:"server_modified"`
}

func walkDropbox(acc Account, emit listingSink) error {
	var payload struct {
		Entries []dropboxEntry `json:"entries"`
		Cursor  string         `json:"cursor"`
		HasMore bool           `json:"has_more"`
	}
	err := postJSON(dropboxAPIBase+"/2/files/list_folder", acc.AccessToken, map[string]any{
		"path": "", "recursive": true, "limit": 2000, "include_deleted": false,
	}, &payload)
	for page := 0; ; page++ {
		if err != nil {
			return fmt.Errorf("dropbox list: %w", err)
		}
		for _, e := range payload.Entries {
			kind := "file"
			switch e.Tag {
			case "folder":
				kind = "folder"
			case "file":
			default:
				continue
			}
			parent := ""
			if i := strings.LastIndex(e.Path, "/"); i > 0 {
				parent = e.Path[:i]
			}
			if err := emit(listingRow{Type: kind, ID: e.ID, Name: e.Name, Path: e.Path, Size: e.Size, Modified: e.Modified, Parent: parent}); err != nil {
				return err
			}
		}
		if !payload.HasMore {
			return nil
		}
		if page+1 >= maxListingPages {
			return fmt.Errorf("dropbox list: stopped after %d pages", maxListingPages)
		}
		cursor := payload.Cursor
		payload.Entries, payload.Cursor, payload.HasMore = nil, "", false
		err = postJSON(dropboxAPIBase+"/2/files/list_folder/continue", acc.AccessToken, map[string]any{"cursor": cursor}, &payload)
	}
}

func walkOneDrive(acc Account, emit listingSink) error {
	// The delta feed is the one Graph call that lists a whole drive, not one folder.
	next := graphAPIBase + "/v1.0/me/drive/root/delta?$top=1000&$select=id,name,size,file,folder,lastModifiedDateTime,parentReference"
	for page := 0; next != "" && page < maxListingPages; page++ {
		var payload struct {
			NextLink string `json:"@odata.nextLink"`
			Value    []struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Size     int64  `json:"size"`
				Modified string `json:"lastModifiedDateTime"`
				File     *struct {
					MimeType string `json:"mimeType"`
				} `json:"file"`
				Folder          *struct{} `json:"folder"`
				ParentReference struct {
					ID   string `json:"id"`
					Path string `json:"path"`
				} `json:"parentReference"`
				Deleted *struct{} `json:"deleted"`
			} `json:"value"`
		}
		if err := getJSON(next, acc.AccessToken, &payload); err != nil {
			return fmt.Errorf("onedrive list: %w", err)
		}
		for _, it := range payload.Value {
			if it.Deleted != nil || (it.File == nil && it.Folder == nil) {
				continue
			}
			row := listingRow{Type: "file", ID: it.ID, Name: it.Name, Size: it.Size, Modified: it.Modified, Parent: it.ParentReference.ID}
			if it.Folder != nil {
				row.Type = "folder"
			}
			if it.File != nil {
				row.Mime = it.File.MimeType
			}
			if dir := strings.TrimPrefix(it.ParentReference.Path, "/drive/root:"); it.ParentReference.Path != "" {
				row.Path = dir + "/" + it.Name
			}
			if err := emit(row); err != nil {
				return err
			}
		}
		next = payload.NextLink
	}
	if next != "" {
		return fmt.Errorf("onedrive list: stopped after %d pages", maxListingPages)
	}
	return nil
}
