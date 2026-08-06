// Command fetch-posters looks up each movie folder on TMDb by its title
// and year, downloads the matching poster into that folder, and saves the
// matched TMDb result as tmdb.json (see CLAUDE.md for that file's shape).
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

		hasPoster := library.FindPoster(dir) != ""
		jsonPath := filepath.Join(dir, "tmdb.json")
		hasJSON := fileExists(jsonPath)

		if hasPoster && hasJSON {
			fmt.Printf("skip  %s (poster + metadata already present)\n", name)
			continue
		}

		var match *tmdbMovie
		var err error
		if id, ok := readTMDbID(dir); ok {
			match, err = getMovieByID(apiKey, id)
		} else {
			title, year := library.ParseTitleYear(name)
			match, err = searchMovie(apiKey, title, year)
		}
		if err != nil {
			fmt.Printf("error %s: %v\n", name, err)
			continue
		}
		if match == nil {
			fmt.Printf("miss  %s (no TMDb match)\n", name)
			continue
		}

		if !hasJSON {
			if err := saveJSON(match, jsonPath); err != nil {
				fmt.Printf("error %s: saving tmdb.json: %v\n", name, err)
			}
		}
		if !hasPoster {
			if match.PosterPath == "" {
				fmt.Printf("miss  %s (TMDb has no poster)\n", name)
			} else if err := downloadPoster(match.PosterPath, dir); err != nil {
				fmt.Printf("error %s: downloading poster: %v\n", name, err)
			}
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readTMDbID reads a manually pinned TMDb movie ID from tmdb_id.txt in dir,
// used to disambiguate short/generic titles that TMDb search matches poorly.
func readTMDbID(dir string) (int, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "tmdb_id.txt"))
	if err != nil {
		return 0, false
	}
	id, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return id, true
}

// tmdbMovie mirrors a single result from TMDb's /search/movie endpoint.
// It's also the shape saved as tmdb.json in a movie's folder (see
// CLAUDE.md) — one flattened object, not the raw search-response wrapper.
type tmdbMovie struct {
	Adult            bool    `json:"adult"`
	BackdropPath     string  `json:"backdrop_path"`
	GenreIDs         []int   `json:"genre_ids"`
	ID               int     `json:"id"`
	OriginalLanguage string  `json:"original_language"`
	OriginalTitle    string  `json:"original_title"`
	Overview         string  `json:"overview"`
	Popularity       float64 `json:"popularity"`
	PosterPath       string  `json:"poster_path"`
	ReleaseDate      string  `json:"release_date"`
	Title            string  `json:"title"`
	Video            bool    `json:"video"`
	VoteAverage      float64 `json:"vote_average"`
	VoteCount        int     `json:"vote_count"`
}

type tmdbSearchResponse struct {
	Results []tmdbMovie `json:"results"`
}

// searchMovie queries TMDb for title (optionally narrowed by year) and
// returns the best-matching result, or nil if there's no match.
func searchMovie(apiKey, title string, year int) (*tmdbMovie, error) {
	q := url.Values{}
	q.Set("query", title)
	if year > 0 {
		q.Set("year", strconv.Itoa(year))
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.themoviedb.org/3/search/movie?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDb search returned %s", resp.Status)
	}

	var parsed tmdbSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Results) == 0 {
		return nil, nil
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
	return &best, nil
}

// getMovieByID fetches a movie directly by TMDb ID, bypassing search.
func getMovieByID(apiKey string, id int) (*tmdbMovie, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://api.themoviedb.org/3/movie/%d", id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDb movie lookup returned %s", resp.Status)
	}

	var detail struct {
		tmdbMovie
		Genres []struct {
			ID int `json:"id"`
		} `json:"genres"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, err
	}
	match := detail.tmdbMovie
	for _, g := range detail.Genres {
		match.GenreIDs = append(match.GenreIDs, g.ID)
	}
	return &match, nil
}

func saveJSON(match *tmdbMovie, path string) error {
	data, err := json.MarshalIndent(match, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func downloadPoster(posterPath, dir string) error {
	posterURL := "https://image.tmdb.org/t/p/w500" + posterPath
	resp, err := http.Get(posterURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}

	ext := filepath.Ext(posterPath)
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
