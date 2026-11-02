package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testVideo = "/series-media/Breaking%20Bad/S01/e01.mp4"

func getPosition(t *testing.T, s *progressStore, profile string) string {
	t.Helper()
	q := url.Values{"profile": {profile}, "video": {testVideo}}
	rec := httptest.NewRecorder()
	s.handler(rec, httptest.NewRequest(http.MethodGet, "/api/progress?"+q.Encode(), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status %d", rec.Code)
	}
	return strings.TrimSpace(rec.Body.String())
}

func post(s *progressStore, body string) int {
	rec := httptest.NewRecorder()
	s.handler(rec, httptest.NewRequest(http.MethodPost, "/api/progress", strings.NewReader(body)))
	return rec.Code
}

func TestProgressIsPerProfileAndSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	s, err := loadProgressStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if code := post(s, `{"profile":"Kevin ","video":"`+testVideo+`","position":300}`); code != http.StatusNoContent {
		t.Fatalf("POST status %d", code)
	}

	reloaded, err := loadProgressStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := getPosition(t, reloaded, "kevin"); got != `{"position":300}` {
		t.Errorf("same profile, different spelling: got %s", got)
	}
	if got := getPosition(t, reloaded, "someone else"); got != `{"position":0}` {
		t.Errorf("other profile: got %s", got)
	}
	if got := getPosition(t, reloaded, ""); got != `{"position":0}` {
		t.Errorf("blank profile: got %s", got)
	}

}

func TestProgressListsEntriesForOneProfile(t *testing.T) {
	s, err := loadProgressStore(filepath.Join(t.TempDir(), "progress.json"))
	if err != nil {
		t.Fatal(err)
	}
	post(s, `{"profile":"kevin","video":"`+testVideo+`","position":3000,"duration":3000}`)
	post(s, `{"profile":"someone else","video":"/media/other.mp4","position":10,"duration":100}`)

	rec := httptest.NewRecorder()
	s.handler(rec, httptest.NewRequest(http.MethodGet, "/api/progress?profile=KEVIN", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status %d", rec.Code)
	}
	var entries map[string]progressEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}
	if e := entries[testVideo]; e.Position != 3000 || e.Duration != 3000 {
		t.Errorf("finished video should be kept with its duration: %+v", e)
	}
}

func TestProgressRejectsBadInput(t *testing.T) {
	s, err := loadProgressStore(filepath.Join(t.TempDir(), "progress.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`not json`,
		`{"video":"/etc/passwd","position":1}`,
		`{"video":"` + testVideo + `","position":-1}`,
		`{"video":"` + testVideo + `","position":1,"duration":-1}`,
		`{"profile":"` + strings.Repeat("x", maxProfileLength+1) + `","video":"` + testVideo + `","position":1}`,
	} {
		if code := post(s, body); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, code)
		}
	}
}

func TestLoadRefusesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	if err := os.WriteFile(path, []byte("{truncated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProgressStore(path); err == nil {
		t.Fatal("expected an error for a corrupt file")
	}
}
