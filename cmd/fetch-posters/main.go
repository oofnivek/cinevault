// Command fetch-posters looks up each movie folder on TMDb by its title
// and year, downloads the matching poster into that folder, and saves the
// matched TMDb result as tmdb.json (see CLAUDE.md for that file's shape).
//
// Usage: TMDB_API_KEY=... MOVIES_DIR=... go run ./cmd/fetch-posters
// (or `make fetch-posters`, which sources .env for you)
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"movie-collection/internal/library"
)

func main() {
	apiKey := os.Getenv("TMDB_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "TMDB_API_KEY is not set — get a v4 Read Access Token at https://www.themoviedb.org/settings/api and add it to .env")
		os.Exit(1)
	}

	moviesDir := os.Getenv("MOVIES_DIR")
	if moviesDir == "" {
		moviesDir = "movies"
	}

	entries, err := os.ReadDir(moviesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading %s: %v\n", moviesDir, err)
		os.Exit(1)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		dir := filepath.Join(moviesDir, name)

		if !hasMP4(dir) {
			continue
		}

		result, err := library.FetchMovie(apiKey, dir, name)
		switch {
		case err != nil:
			fmt.Printf("error %s: %v\n", name, err)
			continue
		case result.Skipped:
			fmt.Printf("skip  %s (poster + metadata already present)\n", name)
			continue
		case result.NoMatch:
			fmt.Printf("miss  %s (no TMDb match)\n", name)
			continue
		case result.NoPoster:
			fmt.Printf("miss  %s (TMDb has no poster)\n", name)
		default:
			fmt.Printf("saved %s\n", name)
		}

		time.Sleep(250 * time.Millisecond) // be polite to TMDb's rate limit
	}
}

func hasMP4(dir string) bool {
	files, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, f := range files {
		if !f.IsDir() && strings.EqualFold(filepath.Ext(f.Name()), ".mp4") {
			return true
		}
	}
	return false
}
