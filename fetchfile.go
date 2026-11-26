package party

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const maxFetchFile = 100 << 20

// FetchDebugFile reads a credentials.json written by the debug dump,
// downloads one file from that cloud by its id, and saves it next to the
// credentials file. It returns the saved path.
func FetchDebugFile(credentialsPath, fileID string) (string, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return "", fmt.Errorf("file id is empty")
	}
	raw, err := os.ReadFile(credentialsPath)
	if err != nil {
		return "", err
	}
	var cred debugCredentials
	if err := json.Unmarshal(raw, &cred); err != nil {
		return "", fmt.Errorf("%s: %w", credentialsPath, err)
	}
	if cred.Source == "" {
		return "", fmt.Errorf("%s has no source", credentialsPath)
	}
	useClientFromCredentials(cred)
	acc := accountFromDebugCredentials(cred)

	// Refresh first if the stored token has expired, and once more if the
	// cloud still says 401 (the stored expiry can be wrong).
	if fresh, changed := refreshAccount(acc, false); changed {
		acc = fresh
		saveRefreshedCredentials(credentialsPath, cred, acc)
	}
	data, name, contentType, err := downloadCloudFile(acc, fileID)
	if err != nil && strings.Contains(err.Error(), "401") {
		if fresh, changed := refreshAccount(acc, true); changed {
			acc = fresh
			saveRefreshedCredentials(credentialsPath, cred, acc)
			data, name, contentType, err = downloadCloudFile(acc, fileID)
		}
	}
	if err != nil {
		return "", err
	}

	out := filepath.Join(filepath.Dir(credentialsPath), fetchedFileName(fileID, name, contentType))
	if err := os.WriteFile(out, data, 0o600); err != nil {
		return "", err
	}
	logf("saved %s (%d bytes)", out, len(data))
	return out, nil
}

func accountFromDebugCredentials(c debugCredentials) Account {
	acc := Account{
		Source:       c.Source,
		UserID:       c.UserID,
		UserName:     c.UserName,
		AccessToken:  c.AccessToken,
		RefreshToken: c.RefreshToken,
	}
	if t, err := time.Parse(time.RFC3339, c.ExpiresAt); err == nil {
		acc.ExpiresAt = t
	}
	return acc
}

// useClientFromCredentials lets getfile run on a machine whose environment has
// no cloud keys: the ones saved with the credentials are used for refreshing.
func useClientFromCredentials(c debugCredentials) {
	idVar, secretVar := "", ""
	switch c.Source {
	case sourceGoogle:
		idVar, secretVar = "PARTY_GOOGLE_CLIENT_ID", "PARTY_GOOGLE_CLIENT_SECRET"
	case sourceDropbox:
		idVar, secretVar = "PARTY_DROPBOX_APP_KEY", "PARTY_DROPBOX_APP_SECRET"
	case sourceOneDrive:
		idVar, secretVar = "PARTY_MICROSOFT_CLIENT_ID", "PARTY_MICROSOFT_CLIENT_SECRET"
	default:
		return
	}
	if os.Getenv(idVar) == "" && c.ClientID != "" {
		_ = os.Setenv(idVar, c.ClientID)
	}
	if os.Getenv(secretVar) == "" && c.ClientSecret != "" {
		_ = os.Setenv(secretVar, c.ClientSecret)
	}
}

func saveRefreshedCredentials(credentialsPath string, cred debugCredentials, acc Account) {
	cred.AccessToken = acc.AccessToken
	cred.RefreshToken = acc.RefreshToken
	if !acc.ExpiresAt.IsZero() {
		cred.ExpiresAt = acc.ExpiresAt.UTC().Format(time.RFC3339)
	}
	raw, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return
	}
	// Some clouds rotate refresh tokens, so keep the newest pair.
	if err := os.WriteFile(credentialsPath, raw, 0o600); err != nil {
		logf("getfile: could not update %s: %v", credentialsPath, err)
	}
}

// fetchedFileName is "<id>.<ext>", never a path, so a hostile id or file name
// from the cloud cannot write outside the credentials folder.
func fetchedFileName(fileID, name, contentType string) string {
	ext := strings.ToLower(path.Ext(strings.ReplaceAll(name, `\`, "/")))
	if ext == "" || len(ext) > 10 || strings.ContainsAny(ext, "/\\ ") {
		ext = ""
		if mt, _, err := mime.ParseMediaType(contentType); err == nil {
			if exts, _ := mime.ExtensionsByType(mt); len(exts) > 0 {
				ext = exts[len(exts)-1]
				for _, e := range exts {
					if e == ".jpg" || e == ".png" || e == ".pdf" || e == ".txt" {
						ext = e
					}
				}
			}
		}
	}
	if ext == "" {
		ext = ".bin"
	}
	return safePathPart(fileID) + ext
}

func downloadCloudFile(acc Account, id string) (data []byte, name, contentType string, err error) {
	switch acc.Source {
	case sourceGoogle:
		return downloadGoogle(acc, id)
	case sourceDropbox:
		return downloadDropbox(acc, id)
	case sourceOneDrive:
		u := graphAPIBase + "/v1.0/me/drive/items/" + url.PathEscape(id) + "/content"
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
		// Graph answers with a redirect to a pre-signed URL; Go drops the
		// Authorization header when that URL is on another host.
		data, contentType, err = readResponse(req)
		return data, "", contentType, err
	}
	return nil, "", "", fmt.Errorf("unknown cloud %q", acc.Source)
}

func downloadGoogle(acc Account, id string) ([]byte, string, string, error) {
	var meta struct {
		Name     string `json:"name"`
		MimeType string `json:"mimeType"`
	}
	base := googleAPIBase + "/drive/v3/files/" + url.PathEscape(id)
	if err := getJSON(base+"?fields=name,mimeType", acc.AccessToken, &meta); err != nil {
		return nil, "", "", err
	}
	target := base + "?alt=media"
	name := meta.Name
	if strings.HasPrefix(meta.MimeType, "application/vnd.google-apps.") {
		// Docs, Sheets and Slides have no bytes of their own: export a PDF.
		target = base + "/export?mimeType=" + url.QueryEscape("application/pdf")
		name += ".pdf"
	}
	req, _ := http.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	data, ct, err := readResponse(req)
	return data, name, ct, err
}

func downloadDropbox(acc Account, id string) ([]byte, string, string, error) {
	arg, _ := json.Marshal(map[string]string{"path": id})
	req, _ := http.NewRequest(http.MethodPost, dropboxContentURL+"/2/files/download", nil)
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	req.Header.Set("Dropbox-API-Arg", string(arg))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	data, err := readBody(resp)
	if err != nil {
		return nil, "", "", err
	}
	var result struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal([]byte(resp.Header.Get("Dropbox-API-Result")), &result)
	return data, result.Name, resp.Header.Get("Content-Type"), nil
}

func readResponse(req *http.Request) ([]byte, string, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := readBody(resp)
	return data, resp.Header.Get("Content-Type"), err
}

func readBody(resp *http.Response) ([]byte, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("download %s: %s", resp.Status, bytesPreview(body))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFetchFile {
		return nil, fmt.Errorf("file is larger than %d MB", maxFetchFile>>20)
	}
	return data, nil
}
