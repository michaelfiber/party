package party

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const (
	dataDir     = "data"
	photosDir   = "data/photos"
	partiesFile = "data/parties.json"
	shotsFile   = "data/shots.json"
)

var (
	storeMu       sync.Mutex
	parties       = map[string]*Party{}
	partiesByCode = map[string]*Party{}
	shots         = map[string]*Shot{}
)

func loadStore() {
	_ = os.MkdirAll(dataDir, 0o755)
	_ = os.MkdirAll(photosDir, 0o755)
	storeMu.Lock()
	parties = map[string]*Party{}
	partiesByCode = map[string]*Party{}
	shots = map[string]*Shot{}
	storeMu.Unlock()
	loadParties()
	loadShots()
}

func loadJSON(path string, dest any) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		logf("store: ignoring corrupt %s: %v", path, err)
	}
}

func saveJSON(path string, v any) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadParties() {
	var list []Party
	loadJSON(partiesFile, &list)
	storeMu.Lock()
	defer storeMu.Unlock()
	for i := range list {
		p := list[i]
		cp := p
		parties[p.ID] = &cp
		if p.Code != "" {
			partiesByCode[p.Code] = &cp
		}
	}
}

func persistParties() {
	storeMu.Lock()
	list := make([]Party, 0, len(parties))
	for _, p := range parties {
		if p != nil {
			list = append(list, *p)
		}
	}
	storeMu.Unlock()
	if err := saveJSON(partiesFile, list); err != nil {
		logf("parties save: %v", err)
	}
}

func loadShots() {
	var list []Shot
	loadJSON(shotsFile, &list)
	storeMu.Lock()
	defer storeMu.Unlock()
	for i := range list {
		s := list[i]
		cp := s
		shots[s.ID] = &cp
	}
}

func persistShots() {
	storeMu.Lock()
	list := make([]Shot, 0, len(shots))
	for _, s := range shots {
		if s != nil {
			list = append(list, *s)
		}
	}
	storeMu.Unlock()
	if err := saveJSON(shotsFile, list); err != nil {
		logf("shots save: %v", err)
	}
}

func getParty(id string) *Party {
	storeMu.Lock()
	defer storeMu.Unlock()
	p := parties[id]
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

func getPartyByCode(code string) *Party {
	storeMu.Lock()
	defer storeMu.Unlock()
	p := partiesByCode[code]
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

func putParty(p *Party) {
	if p == nil || p.ID == "" {
		return
	}
	storeMu.Lock()
	cp := *p
	parties[p.ID] = &cp
	if p.Code != "" {
		partiesByCode[p.Code] = &cp
	}
	storeMu.Unlock()
	persistParties()
}

func putShot(s *Shot) {
	if s == nil || s.ID == "" {
		return
	}
	storeMu.Lock()
	cp := *s
	shots[s.ID] = &cp
	storeMu.Unlock()
	persistShots()
}

func shotsForParty(partyID string) []Shot {
	storeMu.Lock()
	defer storeMu.Unlock()
	out := make([]Shot, 0)
	for _, s := range shots {
		if s != nil && s.PartyID == partyID {
			out = append(out, *s)
		}
	}
	return out
}
