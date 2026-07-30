// Command fetch-posters looks up each movie folder on TMDb by its title
// and year, and downloads the matching poster into that folder.
//
// Usage: TMDB_API_KEY=... MOVIES_DIR=... go run ./cmd/fetch-posters
// (or `make fetch-posters`, which sources .env for you)
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
		if library.FindPoster(dir) != "" {
			fmt.Printf("skip  %s (poster already present)\n", name)
			continue
		}

		title, year := library.ParseTitleYear(name)
		posterURL, err := searchPoster(apiKey, title, year)
		if err != nil {
			fmt.Printf("error %s: %v\n", name, err)
			continue
		}
		if posterURL == "" {
			fmt.Printf("miss  %s (no TMDb match)\n", name)
			continue
		}

		if err := downloadPoster(posterURL, dir); err != nil {
			fmt.Printf("error %s: downloading poster: %v\n", name, err)
			continue
		}
		fmt.Printf("saved %s\n", name)

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

type tmdbSearchResponse struct {
	Results []struct {
		PosterPath  string `json:"poster_path"`
		ReleaseDate string `json:"release_date"`
	} `json:"results"`
}

// searchPoster queries TMDb for title (optionally narrowed by year) and
// returns a full poster image URL, or "" if there's no match.
func searchPoster(apiKey, title string, year int) (string, error) {
	q := url.Values{}
	q.Set("query", title)
	if year > 0 {
		q.Set("year", strconv.Itoa(year))
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.themoviedb.org/3/search/movie?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("TMDb search returned %s", resp.Status)
	}

	var parsed tmdbSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if len(parsed.Results) == 0 {
		return "", nil
	}

	best := parsed.Results[0]
	if year > 0 {
		yearStr := strconv.Itoa(year)
		for _, r := range parsed.Results {
			if strings.HasPrefix(r.ReleaseDate, yearStr) {
				best = r
				break
			}
		}
	}
	if best.PosterPath == "" {
		return "", nil
	}
	return "https://image.tmdb.org/t/p/w500" + best.PosterPath, nil
}

func downloadPoster(posterURL, dir string) error {
	resp, err := http.Get(posterURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}

	ext := filepath.Ext(posterURL)
	if ext == "" {
		ext = ".jpg"
	}
	out, err := os.Create(filepath.Join(dir, "poster"+ext))
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}
