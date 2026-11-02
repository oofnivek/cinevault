package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	defaultProfile   = "default"
	maxProfileLength = 64
	maxVideoLength   = 4096
)

type progressEntry struct {
	Position  int       `json:"position"`
	UpdatedAt time.Time `json:"updated_at"`
}

// progressStore holds watch positions as profile -> video URL -> entry,
// persisted as one JSON file. Every change rewrites the file; clients save
// about every 5 seconds per video being watched, and the file stays small.
type progressStore struct {
	mu   sync.Mutex
	path string
	data map[string]map[string]progressEntry
}

// loadProgressStore reads path, treating a missing file as empty. An
// unreadable file is an error rather than a fresh start, so that the next
// save can't overwrite positions that might still be recoverable by hand.
func loadProgressStore(path string) (*progressStore, error) {
	s := &progressStore{path: path, data: map[string]map[string]progressEntry{}}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return s, nil
}

func (s *progressStore) get(profile, video string) (progressEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[profile][video]
	return e, ok
}

func (s *progressStore) set(profile, video string, position int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[profile] == nil {
		s.data[profile] = map[string]progressEntry{}
	}
	s.data[profile][video] = progressEntry{Position: position, UpdatedAt: time.Now().UTC()}
	return s.write()
}

func (s *progressStore) clear(profile, video string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[profile][video]; !ok {
		return nil
	}
	delete(s.data[profile], video)
	if len(s.data[profile]) == 0 {
		delete(s.data, profile)
	}
	return s.write()
}

// write replaces the file via a temp file and rename, so a crash mid-write
// leaves the previous version intact. The caller must hold s.mu.
func (s *progressStore) write() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once the rename has succeeded
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	// CreateTemp makes the file 0600; keep it readable for backups.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// normalizeProfile makes names typed on different devices match: phones
// tend to capitalize the first letter and autocomplete a trailing space.
// A blank name is a shared default profile, so resume works without one.
func normalizeProfile(name string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return defaultProfile, true
	}
	return name, utf8.RuneCountInString(name) <= maxProfileLength
}

// validVideoKey limits keys to this server's media URLs, since the endpoint
// has no authentication and every entry is persisted.
func validVideoKey(video string) bool {
	return len(video) <= maxVideoLength &&
		(strings.HasPrefix(video, "/media/") || strings.HasPrefix(video, "/series-media/"))
}

// handler serves /api/progress:
//
//	GET    ?profile=&video=                 -> {"position": seconds}, 0 if none
//	POST   {"profile","video","position"}   saves a position
//	DELETE ?profile=&video=                 clears a position
func (s *progressStore) handler(w http.ResponseWriter, r *http.Request) {
	var profile, video string
	var position int
	switch r.Method {
	case http.MethodGet, http.MethodDelete:
		profile, video = r.URL.Query().Get("profile"), r.URL.Query().Get("video")
	case http.MethodPost:
		var body struct {
			Profile  string `json:"profile"`
			Video    string `json:"video"`
			Position int    `json:"position"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		profile, video, position = body.Profile, body.Video, body.Position
		if position < 0 {
			http.Error(w, "invalid position", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	profile, ok := normalizeProfile(profile)
	if !ok {
		http.Error(w, "profile name too long", http.StatusBadRequest)
		return
	}
	if !validVideoKey(video) {
		http.Error(w, "invalid video", http.StatusBadRequest)
		return
	}

	var err error
	switch r.Method {
	case http.MethodGet:
		entry, _ := s.get(profile, video)
		// Never cached: another device may have moved the position since.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Position int `json:"position"`
		}{entry.Position})
		return
	case http.MethodPost:
		err = s.set(profile, video, position)
	case http.MethodDelete:
		err = s.clear(profile, video)
	}
	if err != nil {
		log.Printf("saving watch progress: %v", err)
		http.Error(w, "could not save progress", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
