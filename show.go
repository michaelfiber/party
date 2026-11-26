package party

import (
	"crypto/subtle"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
)

// The slideshow is opened on a TV, tablet or laptop with a private link.
// The link carries its own key (not the guest code), so anyone who can take
// photos cannot also browse the whole album, and the host can rotate it.

const maxShowShots = 300

func showURL(r *http.Request, p *Party) string {
	return publicBaseURL(r) + "/show/" + p.ShowKey
}

// ensureShowKey gives older parties a slideshow key the first time the host opens them.
func ensureShowKey(p *Party) {
	if p == nil || p.ShowKey != "" {
		return
	}
	p.ShowKey = newID()
	putParty(p)
}

func getPartyByShowKey(key string) *Party {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	for _, p := range parties {
		if p == nil || p.ShowKey == "" || len(p.ShowKey) != len(key) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(p.ShowKey), []byte(key)) == 1 {
			cp := *p
			return &cp
		}
	}
	return nil
}

func showParty(w http.ResponseWriter, r *http.Request) *Party {
	p := getPartyByShowKey(r.PathValue("key"))
	if p == nil {
		http.Error(w, "this slideshow link has expired", http.StatusNotFound)
	}
	return p
}

func handleShowPage(w http.ResponseWriter, r *http.Request) {
	if getPartyByShowKey(r.PathValue("key")) == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filepath.Join(staticDir, "show.html"))
}

func handleShowFeed(w http.ResponseWriter, r *http.Request) {
	p := showParty(w, r)
	if p == nil {
		return
	}
	list := shotsForParty(p.ID)
	sort.Slice(list, func(i, j int) bool {
		return list[i].Created.Before(list[j].Created)
	})
	if len(list) > maxShowShots {
		list = list[len(list)-maxShowShots:]
	}
	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		out = append(out, map[string]any{
			"id":      s.ID,
			"facing":  s.Facing,
			"created": s.Created,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"name":    p.Name,
		"count":   p.PhotoCount,
		"joinUrl": joinURL(r, p.Code),
		"shots":   out,
	})
}

func handleShowShot(w http.ResponseWriter, r *http.Request) {
	p := showParty(w, r)
	if p == nil {
		return
	}
	storeMu.Lock()
	s := shots[r.PathValue("id")]
	storeMu.Unlock()
	if s == nil || s.PartyID != p.ID || s.Debug {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, s.Path)
}

// handleShowJoinQR draws the guest camera QR for the corner of the slideshow.
func handleShowJoinQR(w http.ResponseWriter, r *http.Request) {
	p := showParty(w, r)
	if p == nil {
		return
	}
	writeQR(w, joinURL(r, p.Code))
}

// handleShowQR draws the slideshow link for the host to scan with a tablet.
func handleShowQR(w http.ResponseWriter, r *http.Request) {
	p := hostParty(w, r)
	if p == nil {
		return
	}
	ensureShowKey(p)
	writeQR(w, showURL(r, p))
}

func handleShowReset(w http.ResponseWriter, r *http.Request) {
	p := hostParty(w, r)
	if p == nil {
		return
	}
	p.ShowKey = newID()
	putParty(p)
	writeJSON(w, http.StatusOK, hostStatus(r, p))
}
