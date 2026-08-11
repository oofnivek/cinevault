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
// Cast is only ever populated by GetMovieByID (a plain /search/movie
// result doesn't carry credits), flattened up from that response's
// "credits.cast" — crew is deliberately left out, same spirit as season
// tmdb.json leaving out its "episodes" list (see CLAUDE.md).
type TMDbMovie struct {
	Adult            bool             `json:"adult"`
	BackdropPath     string           `json:"backdrop_path"`
	GenreIDs         []int            `json:"genre_ids"`
	ID               int              `json:"id"`
	OriginalLanguage string           `json:"original_language"`
	OriginalTitle    string           `json:"original_title"`
	Overview         string           `json:"overview"`
	Popularity       float64          `json:"popularity"`
	PosterPath       string           `json:"poster_path"`
	ReleaseDate      string           `json:"release_date"`
	Title            string           `json:"title"`
	Video            bool             `json:"video"`
	VoteAverage      float64          `json:"vote_average"`
	VoteCount        int              `json:"vote_count"`
	Cast             []TMDbCastMember `json:"cast,omitempty"`
}

// TMDbCastMember is one entry from a movie's "credits.cast" list — an
// actor/actress and the character they played.
type TMDbCastMember struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Character   string `json:"character"`
	Order       int    `json:"order"`
	ProfilePath string `json:"profile_path"`
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

// GetMovieByID fetches a movie directly by TMDb ID, bypassing search, with
// its cast (see TMDbMovie.Cast) folded in via append_to_response=credits —
// one request instead of a separate call to /movie/{id}/credits.
func GetMovieByID(apiKey string, id int) (*TMDbMovie, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://api.themoviedb.org/3/movie/%d?append_to_response=credits", id), nil)
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
		Credits struct {
			Cast []TMDbCastMember `json:"cast"`
		} `json:"credits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, err
	}
	match := detail.TMDbMovie
	for _, g := range detail.Genres {
		match.GenreIDs = append(match.GenreIDs, g.ID)
	}
	match.Cast = detail.Credits.Cast
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
// tmdb_id.txt in dir per CLAUDE.md) on TMDb, saving the match — including
// cast, see TMDbMovie.Cast — as dir/tmdb.json and downloading its poster
// into dir. It's a no-op if dir already has both, unless force is true, in
// which case both are re-fetched and overwritten regardless.
func FetchMovie(apiKey, dir, name string, force bool) (FetchMovieResult, error) {
	hasPoster := FindPoster(dir) != ""
	jsonPath := filepath.Join(dir, "tmdb.json")
	hasJSON := FileExists(jsonPath)
	if !force && hasPoster && hasJSON {
		return FetchMovieResult{Skipped: true}, nil
	}

	id, ok := ReadTMDbID(dir)
	if !ok {
		title, year := ParseTitleYear(name)
		found, err := SearchMovie(apiKey, title, year)
		if err != nil {
			return FetchMovieResult{}, err
		}
		if found == nil {
			return FetchMovieResult{NoMatch: true}, nil
		}
		id = found.ID
	}

	// Always resolve through the detail endpoint (even for a search match)
	// since that's the only one that carries cast — see GetMovieByID.
	match, err := GetMovieByID(apiKey, id)
	if err != nil {
		return FetchMovieResult{}, err
	}
	if match == nil {
		return FetchMovieResult{NoMatch: true}, nil
	}

	if force || !hasJSON {
		if err := SaveJSON(match, jsonPath); err != nil {
			return FetchMovieResult{}, fmt.Errorf("saving tmdb.json: %w", err)
		}
	}
	if force || !hasPoster {
		if match.PosterPath == "" {
			return FetchMovieResult{NoPoster: true}, nil
		}
		if force {
			removePosterFiles(dir)
		}
		if err := DownloadPoster(match.PosterPath, dir); err != nil {
			return FetchMovieResult{}, fmt.Errorf("downloading poster: %w", err)
		}
	}
	return FetchMovieResult{}, nil
}
