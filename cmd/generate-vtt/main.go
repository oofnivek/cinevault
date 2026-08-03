// Command generate-vtt converts each movie's .srt subtitle file (if any) to
// .vtt and saves it alongside the .srt, so the player never has to do that
// conversion on first page load.
//
// Usage: MOVIES_DIR=... go run ./cmd/generate-vtt
// (or `make generate-vtt`, which sources .env for you)
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"movie-collection/internal/library"
)

func main() {
	moviesDir := os.Getenv("MOVIES_DIR")
	if moviesDir == "" {
		moviesDir = "movies"
	}

	processMovies(moviesDir)
}

func processMovies(moviesDir string) {
	entries, err := os.ReadDir(moviesDir)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "reading %s: %v\n", moviesDir, err)
		}
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(moviesDir, e.Name())
		mp4 := findMP4(dir)
		if mp4 == "" {
			continue
		}
		generate(dir, mp4, e.Name())
	}
}

func findMP4(dir string) string {
	files, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, f := range files {
		if !f.IsDir() && strings.EqualFold(filepath.Ext(f.Name()), ".mp4") {
			return f.Name()
		}
	}
	return ""
}

func hasVTT(dir, mp4Name string) bool {
	files, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	base := strings.TrimSuffix(mp4Name, filepath.Ext(mp4Name))
	for _, f := range files {
		if !f.IsDir() && strings.HasPrefix(f.Name(), base) && strings.EqualFold(filepath.Ext(f.Name()), ".vtt") {
			return true
		}
	}
	return false
}

func generate(dir, mp4Name, label string) {
	alreadyHadVTT := hasVTT(dir, mp4Name)
	vtt := library.EnsureSubtitle(dir, mp4Name)
	switch {
	case vtt == "":
		fmt.Printf("none  %s (no .srt found)\n", label)
	case alreadyHadVTT:
		fmt.Printf("skip  %s (.vtt already present)\n", label)
	default:
		fmt.Printf("saved %s\n", label)
	}
}
