package main

import (
	"embed"
	"encoding/json"
	"html/template"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

var (
	homeTmpl  = template.Must(template.ParseFS(templateFS, "templates/home.html"))
	watchTmpl = template.Must(template.ParseFS(templateFS, "templates/watch.html"))
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

func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

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
		MoviesJSON template.JS
	}{MoviesJSON: template.JS(moviesJSON)}
	homeTmpl.Execute(w, data)
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
		data := struct {
			Title       string
			Year        int
			VideoURL    string
			SubtitleURL string
			Overview    string
			Genres      []string
			MP4URL      string
			VTTURL      string
			SRTURL      string
		}{
			Title:    m.Title,
			Year:     m.Year,
			VideoURL: "/media/" + url.PathEscape(m.Name) + "/" + url.PathEscape(m.MP4Name),
			Overview: m.Overview,
			Genres:   m.Genres,
			MP4URL:   "/media/" + url.PathEscape(m.Name) + "/" + url.PathEscape(m.MP4Name),
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

	// .vtt isn't in every system's mime.types, so register it explicitly
	// for the static file servers below to set the right Content-Type.
	mime.AddExtensionType(".vtt", "text/vtt; charset=utf-8")

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/watch/", watchHandler)
	http.Handle("/media/", http.StripPrefix("/media/", http.FileServer(http.Dir(moviesDir))))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("listening on :" + port)
	log.Fatal(http.ListenAndServe(":"+port, logRequests(http.DefaultServeMux)))
}
