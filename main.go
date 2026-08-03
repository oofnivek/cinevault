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
	"strings"
	"sync"

	"movie-collection/internal/library"
)

//go:embed templates/*.html
var templateFS embed.FS

var moviesDir = "movies"

type Movie struct {
	Name         string // e.g. "Iron Man (2008)"
	MP4Name      string // e.g. "Iron Man (2008).mp4"
	SubtitleName string // e.g. "Iron Man (2008).eng.vtt", empty if none available
	PosterName   string // e.g. "poster.jpg", empty if none found
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// moviesCache holds the last scanMovies() result. Scanning walks every
// movie folder on disk (mp4/subtitle/poster lookups included), so for a
// large library it's too expensive to redo on every page load; the cache
// is only invalidated by an explicit refresh (see refreshHandler).
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

func invalidateMoviesCache() {
	moviesCache.Lock()
	moviesCache.loaded = false
	moviesCache.movies = nil
	moviesCache.err = nil
	moviesCache.Unlock()
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
				movies = append(movies, Movie{
					Name:         name,
					MP4Name:      f.Name(),
					SubtitleName: library.EnsureSubtitle(dir, f.Name()),
					PosterName:   library.FindPoster(dir),
				})
				break
			}
		}
	}
	return movies, nil
}

var homeTmpl = template.Must(template.ParseFS(templateFS, "templates/home.html"))

type movieJSON struct {
	Title       string `json:"title"`
	Year        int    `json:"year"`
	Poster      string `json:"poster"`
	VideoURL    string `json:"videoUrl"`
	SubtitleURL string `json:"subtitleUrl"`
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
		title, year := library.ParseTitleYear(m.Name)
		mj := movieJSON{
			Title:    title,
			Year:     year,
			VideoURL: "/media/" + url.PathEscape(m.Name) + "/" + url.PathEscape(m.MP4Name),
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

func refreshHandler(w http.ResponseWriter, r *http.Request) {
	invalidateMoviesCache()
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func main() {
	if dir := os.Getenv("MOVIES_DIR"); dir != "" {
		moviesDir = dir
	}

	// .vtt isn't in every system's mime.types, so register it explicitly
	// for the static file servers below to set the right Content-Type.
	mime.AddExtensionType(".vtt", "text/vtt; charset=utf-8")

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/refresh", refreshHandler)
	http.Handle("/media/", http.StripPrefix("/media/", http.FileServer(http.Dir(moviesDir))))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("listening on :" + port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
