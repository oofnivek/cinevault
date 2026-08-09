// TMDb search/lookup for movies, and the FetchMovie orchestration shared
// by cmd/fetch-posters (bulk) and the web server's single-movie fetch
// endpoint.
package library

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// TMDbMovie mirrors a single result from TMDb's /search/movie endpoint.
// It's also the shape saved as tmdb.json in a movie's folder (see
// CLAUDE.md) — one flattened object, not the raw search-response wrapper.
type TMDbMovie struct {
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
	Results []TMDbMovie `json:"results"`
}

// SearchMovie queries TMDb for title (optionally narrowed by year) and
// returns the best-matching result, or nil if there's no match.
func SearchMovie(apiKey, title string, year int) (*TMDbMovie, error) {
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

// GetMovieByID fetches a movie directly by TMDb ID, bypassing search.
func GetMovieByID(apiKey string, id int) (*TMDbMovie, error) {
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
		TMDbMovie
		Genres []struct {
			ID int `json:"id"`
		} `json:"genres"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, err
	}
	match := detail.TMDbMovie
	for _, g := range detail.Genres {
		match.GenreIDs = append(match.GenreIDs, g.ID)
	}
	return &match, nil
}

// FetchMovieResult describes the outcome of FetchMovie for one movie
// folder.
type FetchMovieResult struct {
	Skipped  bool // poster + tmdb.json already present, nothing done
	NoMatch  bool // TMDb search/lookup had no result
	NoPoster bool // matched, but TMDb has no poster image for it
}

// FetchMovie looks up name (title/year parsed from name, or a pinned
// tmdb_id.txt in dir per CLAUDE.md) on TMDb, saving the match as
// dir/tmdb.json and downloading its poster into dir. It's a no-op if dir
// already has both.
func FetchMovie(apiKey, dir, name string) (FetchMovieResult, error) {
	hasPoster := FindPoster(dir) != ""
	jsonPath := filepath.Join(dir, "tmdb.json")
	hasJSON := FileExists(jsonPath)
	if hasPoster && hasJSON {
		return FetchMovieResult{Skipped: true}, nil
	}

	var match *TMDbMovie
	var err error
	if id, ok := ReadTMDbID(dir); ok {
		match, err = GetMovieByID(apiKey, id)
	} else {
		title, year := ParseTitleYear(name)
		match, err = SearchMovie(apiKey, title, year)
	}
	if err != nil {
		return FetchMovieResult{}, err
	}
	if match == nil {
		return FetchMovieResult{NoMatch: true}, nil
	}

	if !hasJSON {
		if err := SaveJSON(match, jsonPath); err != nil {
			return FetchMovieResult{}, fmt.Errorf("saving tmdb.json: %w", err)
		}
	}
	if !hasPoster {
		if match.PosterPath == "" {
			return FetchMovieResult{NoPoster: true}, nil
		}
		if err := DownloadPoster(match.PosterPath, dir); err != nil {
			return FetchMovieResult{}, fmt.Errorf("downloading poster: %w", err)
		}
	}
	return FetchMovieResult{}, nil
}
