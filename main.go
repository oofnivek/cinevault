package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"movie-collection/internal/library"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed tmdb_genres.json
var tmdbGenresJSON []byte

var genreNames = loadGenreNames()

func loadGenreNames() map[int]string {
	var genres []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(tmdbGenresJSON, &genres); err != nil {
		log.Fatalf("parsing tmdb_genres.json: %v", err)
	}
	names := make(map[int]string, len(genres))
	for _, g := range genres {
		names[g.ID] = g.Name
	}
	return names
}

// tmdbInfo mirrors the single flattened movie object saved as tmdb.json in
// a movie's folder (see CLAUDE.md). Only present once someone has looked
// the movie up and saved its match.
type tmdbInfo struct {
	Title       string `json:"title"`
	Overview    string `json:"overview"`
	GenreIDs    []int  `json:"genre_ids"`
	ReleaseDate string `json:"release_date"` // e.g. "2025-12-16"
}

// readTMDBInfo reads dir/tmdb.json, if present, returning nil otherwise.
func readTMDBInfo(dir string) *tmdbInfo {
	data, err := os.ReadFile(filepath.Join(dir, "tmdb.json"))
	if err != nil {
		return nil
	}
	var info tmdbInfo
	if json.Unmarshal(data, &info) != nil {
		return nil
	}
	return &info
}

func genreNamesFor(ids []int) []string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := genreNames[id]; ok {
			names = append(names, name)
		}
	}
	return names
}

var moviesDir = "movies"
var seriesDir = "series"

type Movie struct {
	Name         string // e.g. "Iron Man (2008)"
	MP4Name      string // e.g. "Iron Man (2008).mp4"
	SubtitleName string // e.g. "Iron Man (2008).eng.vtt", empty if none available
	SRTName      string // e.g. "Iron Man (2008).eng.srt", empty if none available
	PosterName   string // e.g. "poster.jpg", empty if none found

	// Resolved from tmdb.json when present, otherwise from the folder name.
	Title    string
	Year     int
	Overview string   // empty if tmdb.json isn't present
	Genres   []string // empty if tmdb.json isn't present
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// moviesCache holds the last scanMovies() result. Scanning walks every
// movie folder on disk (mp4/subtitle/poster lookups included), so for a
// large library it's too expensive to redo on every page load; restart the
// server to pick up added/removed/renamed movies.
var moviesCache struct {
	sync.Mutex
	movies []Movie
	err    error
	loaded bool
}

func getMovies() ([]Movie, error) {
	moviesCache.Lock()
	defer moviesCache.Unlock()
	if !moviesCache.loaded {
		moviesCache.movies, moviesCache.err = scanMovies()
		moviesCache.loaded = true
	}
	return moviesCache.movies, moviesCache.err
}

func scanMovies() ([]Movie, error) {
	entries, err := os.ReadDir(moviesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var movies []Movie
	for _, e := range entries {
		if !isDir(filepath.Join(moviesDir, e.Name())) {
			continue
		}
		name := e.Name()

		dir := filepath.Join(moviesDir, name)
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.IsDir() && strings.EqualFold(filepath.Ext(f.Name()), ".mp4") {
				title, year := library.ParseTitleYear(name)
				vttName, srtName := library.EnsureSubtitle(dir, f.Name())
				m := Movie{
					Name:         name,
					MP4Name:      f.Name(),
					SubtitleName: vttName,
					SRTName:      srtName,
					PosterName:   library.FindPoster(dir),
					Title:        title,
					Year:         year,
				}
				if info := readTMDBInfo(dir); info != nil {
					if info.Title != "" {
						m.Title = info.Title
					}
					if len(info.ReleaseDate) >= 4 {
						if y, err := strconv.Atoi(info.ReleaseDate[:4]); err == nil {
							m.Year = y
						}
					}
					m.Overview = info.Overview
					m.Genres = genreNamesFor(info.GenreIDs)
				}
				movies = append(movies, m)
				break
			}
		}
	}
	return movies, nil
}

// Episode is one video file within a season folder, e.g.
// "Breaking Bad - s01e01 - Pilot.mp4" under "Breaking Bad/S01/".
type Episode struct {
	Season    int
	Number    int
	Title     string // from the filename, empty if not present
	FileName  string
	StillName string // e.g. "Breaking Bad - s01e01.jpg", empty if none found
}

// Season is a "S01"-style folder within a series folder.
type Season struct {
	Number     int
	DirName    string // e.g. "S01", the actual folder name on disk
	PosterName string // e.g. "poster.jpg", empty if none found
	Episodes   []Episode
}

// Series is a top-level folder under SERIES_DIR, e.g. "Breaking Bad".
type Series struct {
	Name       string
	PosterName string // e.g. "poster.jpg", empty if none found
	Seasons    []Season
}

// seriesCache holds the last scanSeries() result, mirroring moviesCache:
// scanning walks every series/season folder on disk, so it's cached and
// only redone when the server restarts.
var seriesCache struct {
	sync.Mutex
	series []Series
	err    error
	loaded bool
}

func getSeries() ([]Series, error) {
	seriesCache.Lock()
	defer seriesCache.Unlock()
	if !seriesCache.loaded {
		seriesCache.series, seriesCache.err = scanSeries()
		seriesCache.loaded = true
	}
	return seriesCache.series, seriesCache.err
}

func scanSeries() ([]Series, error) {
	entries, err := os.ReadDir(seriesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var list []Series
	for _, e := range entries {
		seriesPath := filepath.Join(seriesDir, e.Name())
		if !isDir(seriesPath) {
			continue
		}

		seasonEntries, err := os.ReadDir(seriesPath)
		if err != nil {
			continue
		}

		var seasons []Season
		for _, se := range seasonEntries {
			seasonPath := filepath.Join(seriesPath, se.Name())
			if !isDir(seasonPath) {
				continue
			}
			seasonNum, ok := library.ParseSeasonDir(se.Name())
			if !ok {
				continue
			}

			files, err := os.ReadDir(seasonPath)
			if err != nil {
				continue
			}
			var episodes []Episode
			for _, f := range files {
				if f.IsDir() || !strings.EqualFold(filepath.Ext(f.Name()), ".mp4") {
					continue
				}
				_, epNum, title, ok := library.ParseEpisodeFile(f.Name())
				if !ok {
					continue
				}
				episodes = append(episodes, Episode{
					Season:    seasonNum,
					Number:    epNum,
					Title:     title,
					FileName:  f.Name(),
					StillName: library.FindStill(seasonPath, f.Name()),
				})
			}
			if len(episodes) == 0 {
				continue
			}
			sort.Slice(episodes, func(i, j int) bool { return episodes[i].Number < episodes[j].Number })
			seasons = append(seasons, Season{
				Number:     seasonNum,
				DirName:    se.Name(),
				PosterName: library.FindPoster(seasonPath),
				Episodes:   episodes,
			})
		}
		if len(seasons) == 0 {
			continue
		}
		sort.Slice(seasons, func(i, j int) bool { return seasons[i].Number < seasons[j].Number })
		list = append(list, Series{Name: e.Name(), PosterName: library.FindPoster(seriesPath), Seasons: seasons})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// Crumb is one link in a page's breadcrumb trail. URL is empty for the
// trail's last entry (the current page), which renders as plain text
// instead of a link.
type Crumb struct {
	Label string
	URL   string
}

// watchPageData is watch.html's data, shared by watchHandler (movies) and
// watchEpisodeHandler (series episodes) — a single named struct so both
// callers populate the same field set, rather than two mismatched
// anonymous structs where a field the template references but one caller
// forgets to set would abort template execution partway through the page.
type watchPageData struct {
	Title       string
	Year        int
	VideoURL    string
	SubtitleURL string
	Overview    string
	Genres      []string
	MP4URL      string
	VTTURL      string
	SRTURL      string
	BrandURL    string
	Breadcrumbs []Crumb
}

var (
	landingTmpl      = template.Must(template.ParseFS(templateFS, "templates/landing.html"))
	moviesTmpl       = template.Must(template.ParseFS(templateFS, "templates/home.html"))
	seriesTmpl       = template.Must(template.ParseFS(templateFS, "templates/series.html"))
	seriesDetailTmpl = template.Must(template.ParseFS(templateFS, "templates/series-detail.html"))
	seasonDetailTmpl = template.Must(template.ParseFS(templateFS, "templates/season-detail.html"))
	watchTmpl        = template.Must(template.ParseFS(templateFS, "templates/watch.html"))
)

type movieJSON struct {
	Title       string   `json:"title"`
	Year        int      `json:"year"`
	Poster      string   `json:"poster"`
	VideoURL    string   `json:"videoUrl"`
	SubtitleURL string   `json:"subtitleUrl"`
	WatchURL    string   `json:"watchUrl"`
	Genres      []string `json:"genres,omitempty"`
}

// landingHandler serves the top-level chooser page (Movies vs. Series).
// Registered at "/", which net/http's ServeMux treats as a catch-all for
// any path not matched by a more specific pattern, so unmatched paths must
// be rejected explicitly here.
func landingHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	movies, err := getMovies()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	series, err := getSeries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		MovieCount  int
		SeriesCount int
	}{MovieCount: len(movies), SeriesCount: len(series)}
	landingTmpl.Execute(w, data)
}

type seriesJSON struct {
	Name         string `json:"name"`
	Poster       string `json:"poster"`
	SeasonCount  int    `json:"seasonCount"`
	EpisodeCount int    `json:"episodeCount"`
	DetailURL    string `json:"detailUrl"`
}

func seriesHandler(w http.ResponseWriter, r *http.Request) {
	list, err := getSeries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	series := make([]seriesJSON, 0, len(list))
	for _, s := range list {
		episodeCount := 0
		for _, season := range s.Seasons {
			episodeCount += len(season.Episodes)
		}
		sj := seriesJSON{
			Name:         s.Name,
			SeasonCount:  len(s.Seasons),
			EpisodeCount: episodeCount,
			DetailURL:    "/series/" + url.PathEscape(s.Name),
		}
		if s.PosterName != "" {
			sj.Poster = "/series-media/" + url.PathEscape(s.Name) + "/" + url.PathEscape(s.PosterName)
		}
		series = append(series, sj)
	}

	data := struct {
		Series      []seriesJSON
		Breadcrumbs []Crumb
	}{
		Series:      series,
		Breadcrumbs: []Crumb{{Label: "Home", URL: "/"}, {Label: "Series"}},
	}
	seriesTmpl.Execute(w, data)
}

// seriesRouteHandler serves everything under "/series/{name}": with just a
// series name it shows season tiles (seriesDetailHandler); with a season
// dir too ("/series/{name}/{season dir}") it shows that season's episode
// list (seasonDetailHandler).
func seriesRouteHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/series/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 2 {
		seasonDetailHandler(w, r, parts[0], parts[1])
		return
	}
	seriesDetailHandler(w, r, parts[0])
}

// seriesDetailHandler serves "/series/{name}": season tiles for one series,
// one per season, each linking to that season's episode list.
func seriesDetailHandler(w http.ResponseWriter, r *http.Request, name string) {
	list, err := getSeries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, s := range list {
		if s.Name != name {
			continue
		}

		type seasonTileView struct {
			Number       int
			Poster       string
			EpisodeCount int
			DetailURL    string
		}

		seasons := make([]seasonTileView, 0, len(s.Seasons))
		for _, season := range s.Seasons {
			sv := seasonTileView{
				Number:       season.Number,
				EpisodeCount: len(season.Episodes),
				DetailURL:    "/series/" + url.PathEscape(s.Name) + "/" + url.PathEscape(season.DirName),
			}
			if season.PosterName != "" {
				sv.Poster = "/series-media/" + url.PathEscape(s.Name) + "/" +
					url.PathEscape(season.DirName) + "/" + url.PathEscape(season.PosterName)
			}
			seasons = append(seasons, sv)
		}

		data := struct {
			Name        string
			Poster      string
			Seasons     []seasonTileView
			Breadcrumbs []Crumb
		}{
			Name:    s.Name,
			Seasons: seasons,
			Breadcrumbs: []Crumb{
				{Label: "Home", URL: "/"},
				{Label: "Series", URL: "/series"},
				{Label: s.Name},
			},
		}
		if s.PosterName != "" {
			data.Poster = "/series-media/" + url.PathEscape(s.Name) + "/" + url.PathEscape(s.PosterName)
		}
		seriesDetailTmpl.Execute(w, data)
		return
	}
	http.NotFound(w, r)
}

// seasonDetailHandler serves "/series/{name}/{season dir}": the episode
// list for one season.
func seasonDetailHandler(w http.ResponseWriter, r *http.Request, name, seasonDir string) {
	list, err := getSeries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, s := range list {
		if s.Name != name {
			continue
		}
		for _, season := range s.Seasons {
			if season.DirName != seasonDir {
				continue
			}

			type episodeView struct {
				Number   int
				Title    string
				WatchURL string
				Still    string
			}
			episodes := make([]episodeView, 0, len(season.Episodes))
			for _, ep := range season.Episodes {
				ev := episodeView{
					Number: ep.Number,
					Title:  ep.Title,
					WatchURL: "/watch-series/" + url.PathEscape(s.Name) + "/" +
						url.PathEscape(season.DirName) + "/" + url.PathEscape(ep.FileName),
				}
				if ep.StillName != "" {
					ev.Still = "/series-media/" + url.PathEscape(s.Name) + "/" +
						url.PathEscape(season.DirName) + "/" + url.PathEscape(ep.StillName)
				}
				episodes = append(episodes, ev)
			}

			data := struct {
				SeriesName  string
				Number      int
				Poster      string
				Episodes    []episodeView
				Breadcrumbs []Crumb
			}{
				SeriesName: s.Name,
				Number:     season.Number,
				Episodes:   episodes,
				Breadcrumbs: []Crumb{
					{Label: "Home", URL: "/"},
					{Label: "Series", URL: "/series"},
					{Label: s.Name, URL: "/series/" + url.PathEscape(s.Name)},
					{Label: fmt.Sprintf("Season %d", season.Number)},
				},
			}
			if season.PosterName != "" {
				data.Poster = "/series-media/" + url.PathEscape(s.Name) + "/" +
					url.PathEscape(season.DirName) + "/" + url.PathEscape(season.PosterName)
			}
			seasonDetailTmpl.Execute(w, data)
			return
		}
	}
	http.NotFound(w, r)
}

// watchEpisodeHandler serves "/watch-series/{series}/{season dir}/{file}",
// reusing the movie watch page/template to play a single episode.
func watchEpisodeHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/watch-series/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	seriesName, seasonDir, fileName := parts[0], parts[1], parts[2]

	list, err := getSeries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, s := range list {
		if s.Name != seriesName {
			continue
		}
		for _, season := range s.Seasons {
			if season.DirName != seasonDir {
				continue
			}
			for _, ep := range season.Episodes {
				if ep.FileName != fileName {
					continue
				}

				title := fmt.Sprintf("%s — S%02dE%02d", s.Name, season.Number, ep.Number)
				if ep.Title != "" {
					title += " · " + ep.Title
				}
				videoURL := "/series-media/" + url.PathEscape(s.Name) + "/" +
					url.PathEscape(season.DirName) + "/" + url.PathEscape(ep.FileName)

				seasonURL := "/series/" + url.PathEscape(s.Name) + "/" + url.PathEscape(season.DirName)
				data := watchPageData{
					Title:    title,
					VideoURL: videoURL,
					MP4URL:   videoURL,
					BrandURL: seasonURL,
					Breadcrumbs: []Crumb{
						{Label: "Home", URL: "/"},
						{Label: "Series", URL: "/series"},
						{Label: s.Name, URL: "/series/" + url.PathEscape(s.Name)},
						{Label: fmt.Sprintf("Season %d", season.Number), URL: seasonURL},
						{Label: fmt.Sprintf("Episode %d", ep.Number)},
					},
				}
				watchTmpl.Execute(w, data)
				return
			}
		}
	}
	http.NotFound(w, r)
}

func moviesHandler(w http.ResponseWriter, r *http.Request) {
	movies, err := getMovies()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	list := make([]movieJSON, 0, len(movies))
	for _, m := range movies {
		mj := movieJSON{
			Title:    m.Title,
			Year:     m.Year,
			VideoURL: "/media/" + url.PathEscape(m.Name) + "/" + url.PathEscape(m.MP4Name),
			WatchURL: "/watch/" + url.PathEscape(m.Name),
			Genres:   m.Genres,
		}
		if m.PosterName != "" {
			mj.Poster = "/media/" + url.PathEscape(m.Name) + "/" + url.PathEscape(m.PosterName)
		}
		if m.SubtitleName != "" {
			mj.SubtitleURL = "/media/" + url.PathEscape(m.Name) + "/" + url.PathEscape(m.SubtitleName)
		}
		list = append(list, mj)
	}

	moviesJSON, err := json.Marshal(list)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Prevent a title like "</script>" from breaking out of the inline <script>.
	moviesJSON = []byte(strings.ReplaceAll(string(moviesJSON), "</", "<\\/"))

	data := struct {
		MoviesJSON  template.JS
		Breadcrumbs []Crumb
	}{
		MoviesJSON:  template.JS(moviesJSON),
		Breadcrumbs: []Crumb{{Label: "Home", URL: "/"}, {Label: "Movies"}},
	}
	moviesTmpl.Execute(w, data)
}

func watchHandler(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/watch/")
	if name == "" {
		http.NotFound(w, r)
		return
	}

	movies, err := getMovies()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, m := range movies {
		if m.Name != name {
			continue
		}
		data := watchPageData{
			Title:    m.Title,
			Year:     m.Year,
			VideoURL: "/media/" + url.PathEscape(m.Name) + "/" + url.PathEscape(m.MP4Name),
			Overview: m.Overview,
			Genres:   m.Genres,
			MP4URL:   "/media/" + url.PathEscape(m.Name) + "/" + url.PathEscape(m.MP4Name),
			BrandURL: "/movies",
			Breadcrumbs: []Crumb{
				{Label: "Home", URL: "/"},
				{Label: "Movies", URL: "/movies"},
				{Label: m.Title},
			},
		}
		if m.SubtitleName != "" {
			data.SubtitleURL = "/media/" + url.PathEscape(m.Name) + "/" + url.PathEscape(m.SubtitleName)
			data.VTTURL = data.SubtitleURL
		}
		if m.SRTName != "" {
			data.SRTURL = "/media/" + url.PathEscape(m.Name) + "/" + url.PathEscape(m.SRTName)
		}
		watchTmpl.Execute(w, data)
		return
	}
	http.NotFound(w, r)
}

// statusRecorder wraps a ResponseWriter to capture the status code and
// bytes written, since http.ResponseWriter doesn't expose either.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// logRequests logs each request's method, path, remote address, status
// code, response size, and duration once it completes.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %s %d %dB %s", r.RemoteAddr, r.Method, r.URL.Path, rec.status, rec.bytes, time.Since(start).Round(time.Millisecond))
	})
}

func main() {
	if dir := os.Getenv("MOVIES_DIR"); dir != "" {
		moviesDir = dir
	}
	if dir := os.Getenv("SERIES_DIR"); dir != "" {
		seriesDir = dir
	}

	// .vtt isn't in every system's mime.types, so register it explicitly
	// for the static file servers below to set the right Content-Type.
	mime.AddExtensionType(".vtt", "text/vtt; charset=utf-8")

	http.HandleFunc("/", landingHandler)
	http.HandleFunc("/movies", moviesHandler)
	http.HandleFunc("/series", seriesHandler)
	http.HandleFunc("/series/", seriesRouteHandler)
	http.HandleFunc("/watch/", watchHandler)
	http.HandleFunc("/watch-series/", watchEpisodeHandler)
	http.Handle("/media/", http.StripPrefix("/media/", http.FileServer(http.Dir(moviesDir))))
	http.Handle("/series-media/", http.StripPrefix("/series-media/", http.FileServer(http.Dir(seriesDir))))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("listening on :" + port)
	log.Fatal(http.ListenAndServe(":"+port, logRequests(http.DefaultServeMux)))
}
