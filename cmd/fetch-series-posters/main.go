// Command fetch-series-posters looks up each series folder on TMDb by its
// name, downloads the matching poster into that folder, and saves the
// matched TMDb result as tmdb.json (see CLAUDE.md for that file's shape).
// It also fetches each season's own poster and metadata — TMDb gives each
// season a poster distinct from the show-level one — into that season's
// folder.
//
// Usage: TMDB_API_KEY=... SERIES_DIR=... go run ./cmd/fetch-series-posters
// (or `make fetch-series-posters`, which sources .env for you)
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"movie-collection/internal/library"
)

func main() {
	apiKey := os.Getenv("TMDB_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "TMDB_API_KEY is not set — get a v4 Read Access Token at https://www.themoviedb.org/settings/api and add it to .env")
		os.Exit(1)
	}

	seriesDir := os.Getenv("SERIES_DIR")
	if seriesDir == "" {
		seriesDir = "series"
	}

	entries, err := os.ReadDir(seriesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading %s: %v\n", seriesDir, err)
		os.Exit(1)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		dir := filepath.Join(seriesDir, name)

		if !hasSeasonDir(dir) {
			continue
		}

		hasPoster := library.FindPoster(dir) != ""
		jsonPath := filepath.Join(dir, "tmdb.json")
		hasJSON := library.FileExists(jsonPath)

		seriesID, ok := readLocalSeriesID(jsonPath)
		needsFetch := !hasPoster || !hasJSON || !ok

		if needsFetch {
			var match *tmdbSeries
			var err error
			if id, ok := library.ReadTMDbID(dir); ok {
				match, err = getSeriesByID(apiKey, id)
			} else {
				match, err = searchSeries(apiKey, name)
			}
			if err != nil {
				fmt.Printf("error %s: %v\n", name, err)
				continue
			}
			if match == nil {
				fmt.Printf("miss  %s (no TMDb match)\n", name)
				continue
			}
			seriesID = match.ID

			if !hasJSON {
				if err := library.SaveJSON(match, jsonPath); err != nil {
					fmt.Printf("error %s: saving tmdb.json: %v\n", name, err)
				}
			}
			if !hasPoster {
				if match.PosterPath == "" {
					fmt.Printf("miss  %s (TMDb has no poster)\n", name)
				} else if err := library.DownloadPoster(match.PosterPath, dir); err != nil {
					fmt.Printf("error %s: downloading poster: %v\n", name, err)
				}
			}
			fmt.Printf("saved %s\n", name)

			time.Sleep(250 * time.Millisecond) // be polite to TMDb's rate limit
		} else {
			fmt.Printf("skip  %s (poster + metadata already present)\n", name)
		}

		fetchSeasonPosters(apiKey, dir, name, seriesID)
	}
}

// readLocalSeriesID reads the "id" field back out of an already-saved
// tmdb.json, so a cached series doesn't need a fresh API call just to learn
// its TMDb ID for season lookups.
func readLocalSeriesID(jsonPath string) (int, bool) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return 0, false
	}
	var v struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(data, &v); err != nil || v.ID == 0 {
		return 0, false
	}
	return v.ID, true
}

// fetchSeasonPosters fetches each season folder's own poster and metadata —
// distinct from the show-level poster — saving them as poster.* and
// tmdb.json inside that season's own folder.
func fetchSeasonPosters(apiKey, seriesDir, seriesName string, seriesID int) {
	entries, err := os.ReadDir(seriesDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		seasonNum, ok := library.ParseSeasonDir(e.Name())
		if !ok {
			continue
		}
		seasonDir := filepath.Join(seriesDir, e.Name())
		label := seriesName + "/" + e.Name()

		hasPoster := library.FindPoster(seasonDir) != ""
		jsonPath := filepath.Join(seasonDir, "tmdb.json")
		hasJSON := library.FileExists(jsonPath)
		if hasPoster && hasJSON {
			fmt.Printf("skip  %s (poster + metadata already present)\n", label)
			continue
		}

		season, err := getSeason(apiKey, seriesID, seasonNum)
		if err != nil {
			fmt.Printf("error %s: %v\n", label, err)
			continue
		}

		if !hasJSON {
			if err := library.SaveJSON(season, jsonPath); err != nil {
				fmt.Printf("error %s: saving tmdb.json: %v\n", label, err)
			}
		}
		if !hasPoster {
			if season.PosterPath == "" {
				fmt.Printf("miss  %s (TMDb has no season poster)\n", label)
			} else if err := library.DownloadPoster(season.PosterPath, seasonDir); err != nil {
				fmt.Printf("error %s: downloading poster: %v\n", label, err)
			}
		}
		fmt.Printf("saved %s\n", label)

		time.Sleep(250 * time.Millisecond) // be polite to TMDb's rate limit
	}
}

// hasSeasonDir reports whether dir contains at least one "S01"-style season
// folder, distinguishing series folders from anything else that might live
// under SERIES_DIR.
func hasSeasonDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			if _, ok := library.ParseSeasonDir(e.Name()); ok {
				return true
			}
		}
	}
	return false
}

// tmdbSeries mirrors a single result from TMDb's /search/tv endpoint. It's
// also the shape saved as tmdb.json in a series' folder (see CLAUDE.md) —
// one flattened object, not the raw search-response wrapper.
type tmdbSeries struct {
	Adult            bool     `json:"adult"`
	BackdropPath     string   `json:"backdrop_path"`
	GenreIDs         []int    `json:"genre_ids"`
	ID               int      `json:"id"`
	OriginCountry    []string `json:"origin_country"`
	OriginalLanguage string   `json:"original_language"`
	OriginalName     string   `json:"original_name"`
	Overview         string   `json:"overview"`
	Popularity       float64  `json:"popularity"`
	PosterPath       string   `json:"poster_path"`
	FirstAirDate     string   `json:"first_air_date"`
	Name             string   `json:"name"`
	VoteAverage      float64  `json:"vote_average"`
	VoteCount        int      `json:"vote_count"`
}

type tmdbSearchResponse struct {
	Results []tmdbSeries `json:"results"`
}

// searchSeries queries TMDb for name and returns the best-matching result,
// or nil if there's no match.
func searchSeries(apiKey, name string) (*tmdbSeries, error) {
	q := url.Values{}
	q.Set("query", name)

	req, err := http.NewRequest(http.MethodGet, "https://api.themoviedb.org/3/search/tv?"+q.Encode(), nil)
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
	return &parsed.Results[0], nil
}

// getSeriesByID fetches a series directly by TMDb ID, bypassing search.
func getSeriesByID(apiKey string, id int) (*tmdbSeries, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://api.themoviedb.org/3/tv/%d", id), nil)
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
		return nil, fmt.Errorf("TMDb series lookup returned %s", resp.Status)
	}

	var detail struct {
		tmdbSeries
		Genres []struct {
			ID int `json:"id"`
		} `json:"genres"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, err
	}
	match := detail.tmdbSeries
	for _, g := range detail.Genres {
		match.GenreIDs = append(match.GenreIDs, g.ID)
	}
	return &match, nil
}

// tmdbSeason mirrors TMDb's /tv/{series_id}/season/{season_number}
// endpoint. It's also the shape saved as tmdb.json in a season's folder
// (see CLAUDE.md). The endpoint's own "episodes" list is deliberately not
// captured here — episode data comes from the folder scan, not TMDb.
type tmdbSeason struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"`
	AirDate      string  `json:"air_date"`
	SeasonNumber int     `json:"season_number"`
	VoteAverage  float64 `json:"vote_average"`
}

// getSeason fetches one season's own metadata/poster, distinct from the
// show-level ones.
func getSeason(apiKey string, seriesID, seasonNumber int) (*tmdbSeason, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://api.themoviedb.org/3/tv/%d/season/%d", seriesID, seasonNumber), nil)
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
		return nil, fmt.Errorf("TMDb season lookup returned %s", resp.Status)
	}

	var season tmdbSeason
	if err := json.NewDecoder(resp.Body).Decode(&season); err != nil {
		return nil, err
	}
	return &season, nil
}
