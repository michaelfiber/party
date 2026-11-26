package party

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	staticDir  = "static"
	listenAddr = ":3066"
	maxShot    = 12 << 20
)

func logf(format string, args ...any) {
	log.Printf(format, args...)
}

func Run() {
	addr := env("PARTY_ADDR")
	if addr == "" {
		addr = listenAddr
	}
	loadStore()
	log.Printf("party listening on http://localhost%s", addr)
	if p := publicPath(); p != "" {
		log.Printf("public url: %s (also serving under %s/)", publicBaseURL(nil), p)
	}
	if err := http.ListenAndServe(addr, appHandler()); err != nil {
		log.Fatal(err)
	}
}

func appHandler() http.Handler {
	inner := http.NewServeMux()
	inner.Handle("/", noCacheStatic(http.FileServer(http.Dir(staticDir))))
	inner.HandleFunc("GET /p/{code}", handleCameraPage)
	inner.HandleFunc("GET /api/status", handleStatus)
	inner.HandleFunc("POST /api/phone/send", handlePhoneSend)
	inner.HandleFunc("POST /api/phone/verify", handlePhoneVerify)
	inner.HandleFunc("POST /api/phone/logout", handlePhoneLogout)
	inner.HandleFunc("GET /api/parties", handleListParties)
	inner.HandleFunc("POST /api/parties/open", handleOpenParty)
	inner.HandleFunc("POST /api/parties/delete", handleDeleteParty)
	inner.HandleFunc("GET /terms", serveStaticPage("terms.html"))
	inner.HandleFunc("GET /privacy", serveStaticPage("privacy.html"))
	inner.HandleFunc("POST /api/party", handleCreateParty)
	inner.HandleFunc("GET /api/oauth/{source}/start", handleOAuthStart)
	inner.HandleFunc("GET /api/oauth/{source}/callback", handleOAuthCallback)
	inner.HandleFunc("POST /api/oauth/{source}/disconnect", handleDisconnect)
	inner.HandleFunc("GET /api/folders", handleListFolders)
	inner.HandleFunc("POST /api/folders", handleCreateFolder)
	inner.HandleFunc("POST /api/folder", handleSelectFolder)
	inner.HandleFunc("GET /api/qr", handleShareQR)
	inner.HandleFunc("POST /api/text-link", handleTextLink)
	inner.HandleFunc("GET /api/shots", handleListShots)
	inner.HandleFunc("GET /api/shot/{id}", handleShotFile)
	inner.HandleFunc("GET /api/join/{code}", handleJoin)
	inner.HandleFunc("POST /api/join/{code}/shot", handleShot)
	inner.HandleFunc("GET /show/{key}", handleShowPage)
	inner.HandleFunc("GET /api/show/{key}", handleShowFeed)
	inner.HandleFunc("GET /api/show/{key}/shot/{id}", handleShowShot)
	inner.HandleFunc("GET /api/show/{key}/qr", handleShowJoinQR)
	inner.HandleFunc("GET /api/show-qr", handleShowQR)
	inner.HandleFunc("POST /api/show/reset", handleShowReset)

	prefix := publicPath()
	if prefix == "" {
		return inner
	}
	outer := http.NewServeMux()
	outer.Handle("/", inner)
	outer.Handle(prefix+"/", http.StripPrefix(prefix, inner))
	outer.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, prefix+"/", http.StatusMovedPermanently)
	})
	return outer
}

func noCacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ext := strings.ToLower(filepath.Ext(r.URL.Path))
		if r.URL.Path == "/" || ext == ".html" || ext == ".js" || ext == ".css" {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	phone := recoverPhone(r)
	if phone == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"needsLogin": true,
			"sources":    configuredSources(),
			"debug":      debugMode(),
		})
		return
	}
	writeJSON(w, http.StatusOK, loginStatus(r, phone, existingHost(r)))
}

func hostStatus(r *http.Request, p *Party) map[string]any {
	ensureShowKey(p)
	cloud := map[string]any{"connected": false}
	if p.Account != nil {
		cloud = map[string]any{
			"connected":  true,
			"source":     p.Account.Source,
			"label":      sourceDisplayName(p.Account.Source),
			"account":    p.Account.UserName,
			"folderId":   p.FolderID,
			"folderName": p.FolderName,
			"folderPath": p.FolderPath,
			"ready":      cloudReady(p),
		}
	}
	return map[string]any{
		"ok":         true,
		"needsParty": false,
		"debug":      debugMode(),
		"sources":    configuredSources(),
		"party": map[string]any{
			"id":         p.ID,
			"code":       p.Code,
			"name":       p.Name,
			"joinUrl":    joinURL(r, p.Code),
			"showUrl":    showURL(r, p),
			"photoCount": p.PhotoCount,
			"createdAt":  p.Created,
			"cloud":      cloud,
		},
	}
}

func handleCreateParty(w http.ResponseWriter, r *http.Request) {
	phone := recoverPhone(r)
	if phone == "" {
		http.Error(w, "verify your phone number first", http.StatusUnauthorized)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
	p := mintParty(w, body.Name, phone)
	notifyDebugNewParty(r, p)
	writeJSON(w, http.StatusOK, loginStatus(r, phone, p))
}

func handleCameraPage(w http.ResponseWriter, r *http.Request) {
	code := strings.ToLower(strings.TrimSpace(r.PathValue("code")))
	if getPartyByCode(code) == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filepath.Join(staticDir, "camera.html"))
}

func handleJoin(w http.ResponseWriter, r *http.Request) {
	code := strings.ToLower(strings.TrimSpace(r.PathValue("code")))
	p := getPartyByCode(code)
	if p == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"name":  p.Name,
		"count": p.PhotoCount,
		"cloud": cloudReady(p),
		"debug": debugMode(),
	})
}

func handleShot(w http.ResponseWriter, r *http.Request) {
	code := strings.ToLower(strings.TrimSpace(r.PathValue("code")))
	p := getPartyByCode(code)
	if p == nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseMultipartForm(maxShot); err != nil {
		http.Error(w, "send a photo", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("photo")
	if err != nil {
		http.Error(w, "send a photo", http.StatusBadRequest)
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxShot+1))
	if err != nil || len(raw) == 0 {
		http.Error(w, "that photo did not arrive", http.StatusBadRequest)
		return
	}
	if len(raw) > maxShot {
		http.Error(w, "that photo is too large", http.StatusRequestEntityTooLarge)
		return
	}
	if http.DetectContentType(raw) != "image/jpeg" && !strings.HasSuffix(strings.ToLower(header.Filename), ".jpg") && !strings.HasSuffix(strings.ToLower(header.Filename), ".jpeg") {
		if !bytesAreJPEG(raw) {
			http.Error(w, "send a JPEG", http.StatusBadRequest)
			return
		}
	}
	facing := strings.TrimSpace(r.FormValue("facing"))
	if facing != "user" {
		facing = "environment"
	}
	debugShot := r.FormValue("debug") == "1" || r.FormValue("debug") == "true"
	if debugShot && !debugMode() {
		http.Error(w, "debug shots are off", http.StatusForbidden)
		return
	}
	name := shotFileName(facing)
	if debugShot {
		name = "debug-" + name
	}
	dir := filepath.Join(photosDir, p.ID)
	if debugShot {
		dir = filepath.Join(dir, "debug")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, "could not save that photo", http.StatusInternalServerError)
		return
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		http.Error(w, "could not save that photo", http.StatusInternalServerError)
		return
	}
	s := &Shot{
		ID:      newID(),
		PartyID: p.ID,
		Name:    name,
		Facing:  facing,
		Debug:   debugShot,
		Path:    path,
		Created: time.Now().UTC(),
	}
	if !debugShot && cloudReady(p) {
		if err := uploadShotToCloud(p, s, raw); err != nil {
			s.CloudErr = err.Error()
			logf("cloud upload %s: %v", p.Code, err)
		} else {
			s.CloudOK = true
		}
	}
	if !debugShot {
		putShot(s)
		p.PhotoCount++
		putParty(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"id":     s.ID,
		"cloud":  s.CloudOK,
		"count":  p.PhotoCount,
		"debug":  debugShot,
		"queued": !s.CloudOK && p.Account != nil && !debugShot,
	})
}

func bytesAreJPEG(raw []byte) bool {
	return len(raw) > 3 && raw[0] == 0xff && raw[1] == 0xd8
}

func handleListShots(w http.ResponseWriter, r *http.Request) {
	p := hostParty(w, r)
	if p == nil {
		return
	}
	list := shotsForParty(p.ID)
	sort.Slice(list, func(i, j int) bool {
		return list[i].Created.After(list[j].Created)
	})
	if len(list) > 48 {
		list = list[:48]
	}
	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		out = append(out, map[string]any{
			"id":      s.ID,
			"name":    s.Name,
			"facing":  s.Facing,
			"created": s.Created,
			"cloud":   s.CloudOK,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "shots": out, "count": p.PhotoCount})
}

func handleShotFile(w http.ResponseWriter, r *http.Request) {
	p := hostParty(w, r)
	if p == nil {
		return
	}
	id := r.PathValue("id")
	storeMu.Lock()
	s := shots[id]
	storeMu.Unlock()
	if s == nil || s.PartyID != p.ID {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, s.Path)
}

func handleTextLink(w http.ResponseWriter, r *http.Request) {
	p := hostParty(w, r)
	if p == nil {
		return
	}
	var body struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	phone, err := normalizePhone(body.Phone)
	if err != nil {
		http.Error(w, "enter a valid phone number", http.StatusBadRequest)
		return
	}
	msg := "You're holding the camera for " + p.Name + ". Open this to snap photos: " + joinURL(r, p.Code)
	if err := sendSMS(phone, msg); err != nil {
		logf("text link: %v", err)
		http.Error(w, "could not send that text", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func serveStaticPage(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(staticDir, name))
	}
}
