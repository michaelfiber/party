package party

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

type Folder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
}

func handleListFolders(w http.ResponseWriter, r *http.Request) {
	p := hostParty(w, r)
	if p == nil {
		return
	}
	acc := maybeRefresh(p)
	if acc == nil {
		http.Error(w, "connect a cloud first", http.StatusBadRequest)
		return
	}
	parent := strings.TrimSpace(r.URL.Query().Get("parent"))
	folders, err := listFolders(*acc, parent)
	if err != nil {
		logf("list folders %s: %v", acc.Source, err)
		http.Error(w, "could not list folders", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "folders": folders, "parent": parent})
}

func handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	p := hostParty(w, r)
	if p == nil {
		return
	}
	acc := maybeRefresh(p)
	if acc == nil {
		http.Error(w, "connect a cloud first", http.StatusBadRequest)
		return
	}
	var body struct {
		Name   string `json:"name"`
		Parent string `json:"parent"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = p.Name
	}
	folder, err := createFolder(*acc, strings.TrimSpace(body.Parent), name)
	if err != nil {
		logf("create folder %s: %v", acc.Source, err)
		http.Error(w, "could not create that folder", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "folder": folder})
}

func handleSelectFolder(w http.ResponseWriter, r *http.Request) {
	p := hostParty(w, r)
	if p == nil {
		return
	}
	if p.Account == nil {
		http.Error(w, "connect a cloud first", http.StatusBadRequest)
		return
	}
	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	p.FolderID = strings.TrimSpace(body.ID)
	p.FolderName = strings.TrimSpace(body.Name)
	p.FolderPath = strings.TrimSpace(body.Path)
	if p.FolderName == "" {
		p.FolderName = p.Name
	}
	putParty(p)
	writeJSON(w, http.StatusOK, hostStatus(r, p))
}

func listFolders(acc Account, parent string) ([]Folder, error) {
	switch acc.Source {
	case sourceGoogle:
		return listGoogleFolders(acc, parent)
	case sourceDropbox:
		return listDropboxFolders(acc, parent)
	case sourceOneDrive:
		return listOneDriveFolders(acc, parent)
	default:
		return nil, fmt.Errorf("unknown cloud")
	}
}

func createFolder(acc Account, parent, name string) (Folder, error) {
	switch acc.Source {
	case sourceGoogle:
		return createGoogleFolder(acc, parent, name)
	case sourceDropbox:
		return createDropboxFolder(acc, parent, name)
	case sourceOneDrive:
		return createOneDriveFolder(acc, parent, name)
	default:
		return Folder{}, fmt.Errorf("unknown cloud")
	}
}

func uploadShotToCloud(p *Party, s *Shot, jpeg []byte) error {
	if p == nil || p.Account == nil {
		return fmt.Errorf("no cloud connected")
	}
	acc := maybeRefresh(p)
	if acc == nil {
		return fmt.Errorf("no cloud connected")
	}
	switch acc.Source {
	case sourceGoogle:
		return uploadGoogle(acc, p, s.Name, jpeg)
	case sourceDropbox:
		return uploadDropbox(acc, p, s.Name, jpeg)
	case sourceOneDrive:
		return uploadOneDrive(acc, p, s.Name, jpeg)
	default:
		return fmt.Errorf("unknown cloud")
	}
}

func listGoogleFolders(acc Account, parent string) ([]Folder, error) {
	if parent == "" {
		parent = "root"
	}
	q := fmt.Sprintf("trashed=false and mimeType = 'application/vnd.google-apps.folder' and '%s' in parents", strings.ReplaceAll(parent, `'`, `\'`))
	u := "https://www.googleapis.com/drive/v3/files?pageSize=100&fields=files(id,name)&q=" + url.QueryEscape(q)
	var payload struct {
		Files []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"files"`
	}
	if err := getJSON(u, acc.AccessToken, &payload); err != nil {
		return nil, err
	}
	out := make([]Folder, 0, len(payload.Files))
	for _, f := range payload.Files {
		out = append(out, Folder{ID: f.ID, Name: f.Name})
	}
	return out, nil
}

func createGoogleFolder(acc Account, parent, name string) (Folder, error) {
	body := map[string]any{
		"name":     name,
		"mimeType": "application/vnd.google-apps.folder",
	}
	if parent != "" && parent != "root" {
		body["parents"] = []string{parent}
	}
	var out struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := postJSON("https://www.googleapis.com/drive/v3/files", acc.AccessToken, body, &out); err != nil {
		return Folder{}, err
	}
	return Folder{ID: out.ID, Name: out.Name}, nil
}

func uploadGoogle(acc *Account, p *Party, filename string, jpeg []byte) error {
	meta := map[string]any{"name": filename, "mimeType": "image/jpeg"}
	if p.FolderID != "" {
		meta["parents"] = []string{p.FolderID}
	}
	raw, _ := json.Marshal(meta)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Type", "application/json; charset=UTF-8")
	pw, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := pw.Write(raw); err != nil {
		return err
	}
	h = make(textproto.MIMEHeader)
	h.Set("Content-Type", "image/jpeg")
	pw, err = mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := pw.Write(jpeg); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, "https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	req.Header.Set("Content-Type", "multipart/related; boundary="+mw.Boundary())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, bytesPreview(body))
	}
	return nil
}

func listDropboxFolders(acc Account, parent string) ([]Folder, error) {
	var payload struct {
		Entries []struct {
			Tag  string `json:".tag"`
			Name string `json:"name"`
			ID   string `json:"id"`
			Path string `json:"path_display"`
		} `json:"entries"`
	}
	body := map[string]any{
		"path":      parent,
		"recursive": false,
	}
	if err := postJSON("https://api.dropboxapi.com/2/files/list_folder", acc.AccessToken, body, &payload); err != nil {
		return nil, err
	}
	out := make([]Folder, 0)
	for _, e := range payload.Entries {
		if e.Tag != "folder" {
			continue
		}
		out = append(out, Folder{ID: e.ID, Name: e.Name, Path: e.Path})
	}
	return out, nil
}

func createDropboxFolder(acc Account, parent, name string) (Folder, error) {
	folderPath := "/" + strings.Trim(name, "/")
	if parent != "" {
		folderPath = strings.TrimRight(parent, "/") + "/" + name
	}
	var out struct {
		Metadata struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Path string `json:"path_display"`
		} `json:"metadata"`
	}
	if err := postJSON("https://api.dropboxapi.com/2/files/create_folder_v2", acc.AccessToken, map[string]any{"path": folderPath, "autorename": true}, &out); err != nil {
		return Folder{}, err
	}
	return Folder{ID: out.Metadata.ID, Name: out.Metadata.Name, Path: out.Metadata.Path}, nil
}

func uploadDropbox(acc *Account, p *Party, filename string, jpeg []byte) error {
	folder := strings.TrimRight(p.FolderPath, "/")
	if folder == "" {
		folder = "/" + strings.TrimSpace(p.Name)
	}
	arg, _ := json.Marshal(map[string]any{
		"path":       folder + "/" + filename,
		"mode":       "add",
		"autorename": true,
		"mute":       true,
	})
	req, err := http.NewRequest(http.MethodPost, "https://content.dropboxapi.com/2/files/upload", bytes.NewReader(jpeg))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Dropbox-API-Arg", string(arg))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, bytesPreview(body))
	}
	return nil
}

func listOneDriveFolders(acc Account, parent string) ([]Folder, error) {
	u := "https://graph.microsoft.com/v1.0/me/drive/root/children?$top=100&$select=id,name,folder"
	if parent != "" {
		u = "https://graph.microsoft.com/v1.0/me/drive/items/" + url.PathEscape(parent) + "/children?$top=100&$select=id,name,folder"
	}
	var payload struct {
		Value []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Folder *struct {
				ChildCount int `json:"childCount"`
			} `json:"folder"`
		} `json:"value"`
	}
	if err := getJSON(u, acc.AccessToken, &payload); err != nil {
		return nil, err
	}
	out := make([]Folder, 0)
	for _, item := range payload.Value {
		if item.Folder == nil {
			continue
		}
		out = append(out, Folder{ID: item.ID, Name: item.Name})
	}
	return out, nil
}

func createOneDriveFolder(acc Account, parent, name string) (Folder, error) {
	u := "https://graph.microsoft.com/v1.0/me/drive/root/children"
	if parent != "" {
		u = "https://graph.microsoft.com/v1.0/me/drive/items/" + url.PathEscape(parent) + "/children"
	}
	var out struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := postJSON(u, acc.AccessToken, map[string]any{"name": name, "folder": map[string]any{}, "@microsoft.graph.conflictBehavior": "rename"}, &out); err != nil {
		return Folder{}, err
	}
	return Folder{ID: out.ID, Name: out.Name}, nil
}

func uploadOneDrive(acc *Account, p *Party, filename string, jpeg []byte) error {
	parent := p.FolderID
	if parent == "" {
		parent = "root"
	}
	u := "https://graph.microsoft.com/v1.0/me/drive/items/" + url.PathEscape(parent) + ":/" + url.PathEscape(filename) + ":/content"
	req, err := http.NewRequest(http.MethodPut, u, bytes.NewReader(jpeg))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	req.Header.Set("Content-Type", "image/jpeg")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, bytesPreview(body))
	}
	return nil
}

func shotFileName(facing string) string {
	stamp := time.Now().UTC().Format("20060102-150405")
	side := "party"
	if facing == "user" {
		side = "selfie"
	}
	return stamp + "-" + side + "-" + strings.TrimSpace(newPartyCode()) + ".jpg"
}

func cloudReady(p *Party) bool {
	if p == nil || p.Account == nil {
		return false
	}
	if p.Account.Source == sourceDropbox {
		return p.FolderPath != "" || p.FolderName != ""
	}
	return p.FolderID != ""
}
