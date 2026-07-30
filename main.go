package main

import (
	"html/template"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	moviesDir = "movies"
	seriesDir = "series"
)

type Movie struct {
	Name         string // e.g. "Iron Man (2008)"
	MP4Name      string // e.g. "Iron Man (2008).mp4"
	SubtitleName string // e.g. "Iron Man (2008).eng.vtt", empty if none available
}

type Episode struct {
	Name         string // e.g. "S01E01 - Pilot.mp4"
	MP4Name      string
	SubtitleName string
}

var srtTimestamp = regexp.MustCompile(`(\d{2}:\d{2}:\d{2}),(\d{3})`)

// srtToVTT converts SRT subtitle content to WebVTT, the only format
// natively supported by HTML5 <track> elements.
func srtToVTT(srt []byte) []byte {
	body := strings.TrimPrefix(string(srt), "\ufeff")
	body = srtTimestamp.ReplaceAllString(body, "$1.$2")
	return []byte("WEBVTT\n\n" + body)
}

// ensureSubtitle looks in dir for a .vtt file matching mp4Name's prefix and
// returns its name if found. Otherwise, if a matching .srt file exists, it
// converts it to .vtt once, writes it alongside the .srt, and returns the
// new file's name. Returns "" if no subtitle is available.
func ensureSubtitle(dir, mp4Name string) string {
	files, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	base := strings.TrimSuffix(mp4Name, filepath.Ext(mp4Name))

	var srtName string
	for _, f := range files {
		if f.IsDir() || !strings.HasPrefix(f.Name(), base) {
			continue
		}
		switch strings.ToLower(filepath.Ext(f.Name())) {
		case ".vtt":
			return f.Name()
		case ".srt":
			srtName = f.Name()
		}
	}
	if srtName == "" {
		return ""
	}

	srtData, err := os.ReadFile(filepath.Join(dir, srtName))
	if err != nil {
		return ""
	}
	vttName := strings.TrimSuffix(srtName, filepath.Ext(srtName)) + ".vtt"
	if err := os.WriteFile(filepath.Join(dir, vttName), srtToVTT(srtData), 0644); err != nil {
		log.Printf("could not write %s: %v", vttName, err)
		return ""
	}
	return vttName
}

type Season struct {
	Name     string // e.g. "Season 1"
	Episodes []Episode
}

type Series struct {
	Name    string // e.g. "Breaking Bad"
	Seasons []Season
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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
				movies = append(movies, Movie{Name: name, MP4Name: f.Name(), SubtitleName: ensureSubtitle(dir, f.Name())})
				break
			}
		}
	}
	return movies, nil
}

func scanSeries() ([]Series, error) {
	showEntries, err := os.ReadDir(seriesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var series []Series
	for _, showEntry := range showEntries {
		if !isDir(filepath.Join(seriesDir, showEntry.Name())) {
			continue
		}
		showName := showEntry.Name()

		seasonEntries, err := os.ReadDir(filepath.Join(seriesDir, showName))
		if err != nil {
			continue
		}

		var seasons []Season
		for _, seasonEntry := range seasonEntries {
			if !isDir(filepath.Join(seriesDir, showName, seasonEntry.Name())) {
				continue
			}
			seasonName := seasonEntry.Name()

			seasonDir := filepath.Join(seriesDir, showName, seasonName)
			episodeFiles, err := os.ReadDir(seasonDir)
			if err != nil {
				continue
			}

			var episodes []Episode
			for _, f := range episodeFiles {
				if !f.IsDir() && strings.EqualFold(filepath.Ext(f.Name()), ".mp4") {
					episodes = append(episodes, Episode{Name: f.Name(), MP4Name: f.Name(), SubtitleName: ensureSubtitle(seasonDir, f.Name())})
				}
			}
			if len(episodes) > 0 {
				seasons = append(seasons, Season{Name: seasonName, Episodes: episodes})
			}
		}
		if len(seasons) > 0 {
			series = append(series, Series{Name: showName, Seasons: seasons})
		}
	}
	return series, nil
}

func findSeason(showName, seasonName string) (*Season, error) {
	allSeries, err := scanSeries()
	if err != nil {
		return nil, err
	}
	for _, s := range allSeries {
		if s.Name != showName {
			continue
		}
		for _, season := range s.Seasons {
			if season.Name == seasonName {
				return &season, nil
			}
		}
	}
	return nil, nil
}

var homeTmpl = template.Must(template.New("home").Parse(`<!DOCTYPE html>
<html>
<head><title>Movie Collection</title></head>
<body>
<h1>Movies</h1>
<ul>
{{range .Movies}}
  <li><a href="/watch/{{.Name}}">{{.Name}}</a></li>
{{end}}
</ul>
<h1>Series</h1>
<ul>
{{range .Series}}
  <li><a href="/series/{{.Name}}">{{.Name}}</a></li>
{{end}}
</ul>
</body>
</html>
`))

var watchTmpl = template.Must(template.New("watch").Parse(`<!DOCTYPE html>
<html>
<head><title>{{.Name}}</title></head>
<body>
<h1>{{.Name}}</h1>
<video src="{{.VideoURL}}" controls width="960">
{{if .SubtitleURL}}<track kind="subtitles" src="{{.SubtitleURL}}" srclang="en" label="English" default>{{end}}
</video>
<p><a href="/">Back to list</a></p>
</body>
</html>
`))

var seasonsTmpl = template.Must(template.New("seasons").Parse(`<!DOCTYPE html>
<html>
<head><title>{{.Name}}</title></head>
<body>
<h1>{{.Name}}</h1>
<ul>
{{range .Seasons}}
  <li><a href="/series/{{$.Name}}/{{.Name}}">{{.Name}}</a></li>
{{end}}
</ul>
<p><a href="/">Back to list</a></p>
</body>
</html>
`))

var episodesTmpl = template.Must(template.New("episodes").Parse(`<!DOCTYPE html>
<html>
<head><title>{{.ShowName}} — {{.SeasonName}}</title></head>
<body>
<h1>{{.ShowName}} — {{.SeasonName}}</h1>
<ul>
{{range .Episodes}}
  <li><a href="/watch-series/{{$.ShowName}}/{{$.SeasonName}}/{{.Name}}">{{.Name}}</a></li>
{{end}}
</ul>
<p><a href="/series/{{.ShowName}}">Back to seasons</a></p>
</body>
</html>
`))

func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	movies, err := scanMovies()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	series, err := scanSeries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Movies []Movie
		Series []Series
	}{Movies: movies, Series: series}
	homeTmpl.Execute(w, data)
}

func watchHandler(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/watch/")
	movies, err := scanMovies()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, m := range movies {
		if m.Name == name {
			data := struct {
				Name        string
				VideoURL    string
				SubtitleURL string
			}{
				Name:     m.Name,
				VideoURL: "/media/" + url.PathEscape(m.Name) + "/" + url.PathEscape(m.MP4Name),
			}
			if m.SubtitleName != "" {
				data.SubtitleURL = "/media/" + url.PathEscape(m.Name) + "/" + url.PathEscape(m.SubtitleName)
			}
			watchTmpl.Execute(w, data)
			return
		}
	}
	http.NotFound(w, r)
}

func seriesHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/series/")
	parts := strings.SplitN(path, "/", 2)
	showName := parts[0]

	if len(parts) == 1 {
		allSeries, err := scanSeries()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, s := range allSeries {
			if s.Name == showName {
				seasonsTmpl.Execute(w, s)
				return
			}
		}
		http.NotFound(w, r)
		return
	}

	seasonName := parts[1]
	season, err := findSeason(showName, seasonName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if season == nil {
		http.NotFound(w, r)
		return
	}

	data := struct {
		ShowName   string
		SeasonName string
		Episodes   []Episode
	}{ShowName: showName, SeasonName: seasonName, Episodes: season.Episodes}
	episodesTmpl.Execute(w, data)
}

func watchEpisodeHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/watch-series/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	showName, seasonName, episodeName := parts[0], parts[1], parts[2]

	season, err := findSeason(showName, seasonName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if season == nil {
		http.NotFound(w, r)
		return
	}

	for _, ep := range season.Episodes {
		if ep.Name == episodeName {
			data := struct {
				Name        string
				VideoURL    string
				SubtitleURL string
			}{
				Name: showName + " — " + seasonName + " — " + ep.Name,
				VideoURL: "/series-media/" + url.PathEscape(showName) + "/" +
					url.PathEscape(seasonName) + "/" + url.PathEscape(ep.MP4Name),
			}
			if ep.SubtitleName != "" {
				data.SubtitleURL = "/series-media/" + url.PathEscape(showName) + "/" +
					url.PathEscape(seasonName) + "/" + url.PathEscape(ep.SubtitleName)
			}
			watchTmpl.Execute(w, data)
			return
		}
	}
	http.NotFound(w, r)
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

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/watch/", watchHandler)
	http.HandleFunc("/series/", seriesHandler)
	http.HandleFunc("/watch-series/", watchEpisodeHandler)
	http.Handle("/media/", http.StripPrefix("/media/", http.FileServer(http.Dir(moviesDir))))
	http.Handle("/series-media/", http.StripPrefix("/series-media/", http.FileServer(http.Dir(seriesDir))))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("listening on :" + port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
