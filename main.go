package main

import (
	"encoding/json"
	"html/template"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	PosterName   string // e.g. "poster.jpg", empty if none found
}

var posterExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}

// findPoster returns the name of an image file in dir to use as a poster,
// preferring one named "poster.*", otherwise the first image file found.
func findPoster(dir string) string {
	files, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var fallback string
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name()))
		if !posterExts[ext] {
			continue
		}
		if strings.ToLower(strings.TrimSuffix(f.Name(), ext)) == "poster" {
			return f.Name()
		}
		if fallback == "" {
			fallback = f.Name()
		}
	}
	return fallback
}

var yearPattern = regexp.MustCompile(`^(.*)\s\((\d{4})\)$`)

// parseTitleYear splits a folder name like "Iron Man (2008)" into its title
// and year. If name doesn't end in "(YYYY)", year is 0.
func parseTitleYear(name string) (title string, year int) {
	m := yearPattern.FindStringSubmatch(name)
	if m == nil {
		return name, 0
	}
	y, _ := strconv.Atoi(m[2])
	return m[1], y
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
				movies = append(movies, Movie{
					Name:         name,
					MP4Name:      f.Name(),
					SubtitleName: ensureSubtitle(dir, f.Name()),
					PosterName:   findPoster(dir),
				})
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
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>CineVault</title>
<style>
  :root {
    --color-bg: #0b0d12;
    --color-surface: #141926;
    --color-surface-hover: #1b2233;
    --color-text: #e6e8ee;
    --color-text-dim: #9aa1b1;
    --color-border: #232a3b;
    --color-accent: #34d3ac;
    --color-accent-strong: #22b795;
    --radius: 10px;
    --radius-lg: 16px;
    --shadow-lg: 0 20px 40px rgba(0,0,0,.5);
    --space-1: 4px; --space-2: 8px; --space-3: 12px;
    --space-4: 16px; --space-6: 24px; --space-8: 32px;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0;
    background: var(--color-bg);
    color: var(--color-text);
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
  }
  a { color: var(--color-accent); }
  ::-webkit-scrollbar { width: 10px; height: 10px; }
  ::-webkit-scrollbar-thumb { background: #2a3245; border-radius: 999px; }

  .nav {
    display: flex; align-items: center; gap: 8px;
    padding: var(--space-4) var(--space-6);
    border-bottom: 1px solid var(--color-border);
    position: sticky; top: 0; background: var(--color-bg); z-index: 10;
  }
  .nav-brand { display: flex; align-items: center; gap: 8px; font-weight: 600; font-size: 18px; color: var(--color-accent); }

  main { padding: var(--space-6); max-width: 1400px; margin: 0 auto; }
  h1.title { font-size: 32px; margin: 0 0 var(--space-1); }
  .subtitle { margin: 0 0 var(--space-4); color: var(--color-text-dim); }

  .toolbar { display: flex; align-items: center; gap: var(--space-3); flex-wrap: wrap; margin-bottom: var(--space-4); }
  .field { position: relative; min-width: 240px; }
  .field svg { position: absolute; left: 14px; top: 50%; transform: translateY(-50%); opacity: 0.6; pointer-events: none; }
  .input {
    background: var(--color-surface); border: 1px solid var(--color-border); color: var(--color-text);
    border-radius: var(--radius); padding: 10px 12px; font-size: 14px; width: 100%;
  }
  .input:focus { outline: 2px solid var(--color-accent); outline-offset: -1px; }
  .field .input { padding-left: 38px; }

  .seg { display: flex; border: 1px solid var(--color-border); border-radius: var(--radius); overflow: hidden; margin-left: auto; }
  .seg label { padding: 8px 14px; font-size: 13px; cursor: pointer; color: var(--color-text-dim); display: flex; align-items: center; gap: 6px; }
  .seg input { accent-color: var(--color-accent); }
  .seg label:not(:last-child) { border-right: 1px solid var(--color-border); }

  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: var(--space-3); }
  .card {
    background: var(--color-surface); border: 1px solid var(--color-border); border-radius: var(--radius-lg);
    padding: 0; cursor: pointer; text-align: left; color: inherit; font: inherit; overflow: hidden;
    transition: transform .12s ease, background .12s ease;
  }
  .card:hover { background: var(--color-surface-hover); transform: translateY(-2px); }
  .card:focus-visible { outline: 2px solid var(--color-accent); outline-offset: 2px; }
  .poster { aspect-ratio: 2/3; overflow: hidden; background: linear-gradient(160deg, #22283a, #141926); display: flex; align-items: center; justify-content: center; }
  .poster img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .poster .fallback { font-size: 42px; font-weight: 700; color: var(--color-text-dim); }
  .card-body { padding: var(--space-2) var(--space-3) var(--space-3); }
  .card-title { font-size: 15px; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
  .card-year { font-size: 13px; color: var(--color-text-dim); margin-top: 2px; }

  .empty { max-width: 420px; padding: var(--space-6) 0; color: var(--color-text-dim); }

  .pagerow { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); margin-top: var(--space-6); }
  .pagerow label { display: flex; align-items: center; gap: var(--space-2); font-size: 13px; color: var(--color-text-dim); }
  .pagination { display: flex; align-items: center; gap: var(--space-2); justify-content: center; margin-top: var(--space-6); }
  .btn { background: transparent; border: 1px solid var(--color-border); color: var(--color-text); border-radius: var(--radius); padding: 8px; cursor: pointer; display: flex; align-items: center; }
  .btn:hover:not(:disabled) { background: var(--color-surface-hover); }
  .btn:disabled { opacity: 0.35; cursor: default; }
  .page-jump { font-size: 13px; color: var(--color-text-dim); display: flex; align-items: center; gap: 6px; }
  .page-jump input { width: 48px; text-align: center; padding: 4px 6px; }

  .player-back { display: flex; align-items: center; gap: 6px; background: transparent; border: none; color: var(--color-text-dim); cursor: pointer; font-size: 14px; padding: 8px 0; margin-bottom: var(--space-4); }
  .player-back:hover { color: var(--color-text); }
  .player-video-wrap { border-radius: var(--radius-lg); overflow: hidden; box-shadow: var(--shadow-lg); background: #000; max-width: 980px; }
  .player-video-wrap video { width: 100%; display: block; aspect-ratio: 16/9; background: #000; }
  .player-title { font-size: 28px; margin: var(--space-4) 0 0; max-width: 980px; }
  .player-year { color: var(--color-text-dim); font-size: 15px; }

  [hidden] { display: none !important; }
</style>
</head>
<body>

<nav class="nav">
  <span class="nav-brand">
    <svg width="20" height="20" viewBox="0 0 256 256" fill="currentColor"><path opacity="0.25" d="M224,80l-96,56L32,80l96-56Z"/><path d="M230.91,172A8,8,0,0,1,228,182.91l-96,56a8,8,0,0,1-8.06,0l-96-56A8,8,0,0,1,36,169.09l92,53.65,92-53.65A8,8,0,0,1,230.91,172ZM220,121.09l-92,53.65L36,121.09A8,8,0,0,0,28,134.91l96,56a8,8,0,0,0,8.06,0l96-56A8,8,0,1,0,220,121.09ZM24,80a8,8,0,0,1,4-6.91l96-56a8,8,0,0,1,8.06,0l96,56a8,8,0,0,1,0,13.82l-96,56a8,8,0,0,1-8.06,0l-96-56A8,8,0,0,1,24,80Zm23.88,0L128,126.74,208.12,80,128,33.26Z"/></svg>
    CineVault
  </span>
</nav>

<main>
  <div id="grid-view">
    <h1 class="title">My Collection</h1>
    <p class="subtitle" id="result-count"></p>

    <div class="toolbar">
      <div class="field">
        <svg width="15" height="15" viewBox="0 0 256 256" fill="currentColor"><path opacity="0.25" d="M168,112a56,56,0,1,1-56-56A56,56,0,0,1,168,112Z"/><path d="M229.66,218.34,175.4,164.08a92.31,92.31,0,1,0-11.32,11.32l54.26,54.26a8,8,0,0,0,11.32-11.32ZM40,112a72,72,0,1,1,72,72A72.08,72.08,0,0,1,40,112Z"/></svg>
        <input class="input" type="text" id="search" placeholder="Search by title…" />
      </div>

      <div class="seg" role="radiogroup" aria-label="Sort by">
        <label><input type="radio" name="sort" value="year" checked />Year</label>
        <label><input type="radio" name="sort" value="name" />Name</label>
      </div>
    </div>

    <div class="grid" id="grid"></div>
    <div class="empty" id="empty" hidden>
      <div class="card-title">No movies match</div>
      <p>Try a different title.</p>
    </div>

    <div class="pagerow">
      <label>
        Per page
        <select class="input" id="page-size" style="width: auto; padding-block: 6px;">
          <option value="12">12</option>
          <option value="24" selected>24</option>
          <option value="48">48</option>
          <option value="96">96</option>
        </select>
      </label>
    </div>

    <div class="pagination" id="pagination" hidden>
      <button type="button" class="btn" id="prev-page" aria-label="Previous page">
        <svg width="16" height="16" viewBox="0 0 256 256" fill="currentColor"><path opacity="0.25" d="M176,48V208L80,128Z"/><path d="M181.66,133.66l-96,96a8,8,0,0,1-11.32-11.32L164.69,128,74.34,37.66a8,8,0,0,1,11.32-11.32l96,96A8,8,0,0,1,181.66,133.66Z" transform="scale(-1,1) translate(-256,0)"/></svg>
      </button>
      <span class="page-jump">
        Page
        <input type="number" class="input" min="1" id="page-input" />
        of <span id="total-pages"></span>
      </span>
      <button type="button" class="btn" id="next-page" aria-label="Next page">
        <svg width="16" height="16" viewBox="0 0 256 256" fill="currentColor"><path opacity="0.25" d="M176,48V208L80,128Z"/><path d="M181.66,133.66l-96,96a8,8,0,0,1-11.32-11.32L164.69,128,74.34,37.66a8,8,0,0,1,11.32-11.32l96,96A8,8,0,0,1,181.66,133.66Z"/></svg>
      </button>
    </div>
  </div>

  <div id="player-view" hidden>
    <button type="button" class="player-back" id="back-btn">
      <svg width="15" height="15" viewBox="0 0 256 256" fill="currentColor"><path opacity="0.25" d="M216,128a8,8,0,0,1-8,8H40a8,8,0,0,1,0-16H208A8,8,0,0,1,216,128Z"/><path d="M181.66,133.66l-80,80a8,8,0,0,1-11.32-11.32L164.69,128,90.34,53.66a8,8,0,0,1,11.32-11.32l80,80A8,8,0,0,1,181.66,133.66Z" transform="scale(-1,1) translate(-256,0)"/></svg>
      Back to collection
    </button>
    <div class="player-video-wrap">
      <video id="player-video" controls></video>
    </div>
    <h1 class="player-title" id="player-title"></h1>
    <div class="player-year" id="player-year"></div>
  </div>
</main>

<script>
const MOVIES = {{.MoviesJSON}};

let query = '';
let sortBy = 'year';
let pageSize = 24;
let page = 1;

const gridEl = document.getElementById('grid');
const emptyEl = document.getElementById('empty');
const resultCountEl = document.getElementById('result-count');
const paginationEl = document.getElementById('pagination');
const totalPagesEl = document.getElementById('total-pages');
const pageInputEl = document.getElementById('page-input');

function filtered() {
  const q = query.trim().toLowerCase();
  let list = MOVIES.filter((m) => !q || m.title.toLowerCase().includes(q));
  list = list.slice().sort((a, b) => sortBy === 'name' ? a.title.localeCompare(b.title) : b.year - a.year);
  return list;
}

function renderGrid() {
  const list = filtered();
  const totalPages = Math.max(1, Math.ceil(list.length / pageSize));
  page = Math.min(Math.max(1, page), totalPages);
  const pageItems = list.slice((page - 1) * pageSize, page * pageSize);

  resultCountEl.textContent = MOVIES.length === 0
    ? 'No movies found'
    : list.length + ' movie' + (list.length === 1 ? '' : 's');

  gridEl.innerHTML = '';
  emptyEl.hidden = !(MOVIES.length > 0 && pageItems.length === 0);

  for (const movie of pageItems) {
    const card = document.createElement('button');
    card.type = 'button';
    card.className = 'card';

    const poster = document.createElement('div');
    poster.className = 'poster';
    if (movie.poster) {
      const img = document.createElement('img');
      img.src = movie.poster;
      img.alt = movie.title;
      img.loading = 'lazy';
      poster.appendChild(img);
    } else {
      const fallback = document.createElement('span');
      fallback.className = 'fallback';
      fallback.textContent = (movie.title[0] || '?').toUpperCase();
      poster.appendChild(fallback);
    }

    const body = document.createElement('div');
    body.className = 'card-body';
    const title = document.createElement('div');
    title.className = 'card-title';
    title.textContent = movie.title;
    const year = document.createElement('div');
    year.className = 'card-year';
    year.textContent = movie.year || '';
    body.append(title, year);

    card.append(poster, body);
    card.addEventListener('click', () => openPlayer(movie));
    gridEl.appendChild(card);
  }

  paginationEl.hidden = totalPages <= 1;
  totalPagesEl.textContent = totalPages;
  pageInputEl.max = totalPages;
  pageInputEl.value = page;
  document.getElementById('prev-page').disabled = page === 1;
  document.getElementById('next-page').disabled = page === totalPages;
}

function openPlayer(movie) {
  document.getElementById('grid-view').hidden = true;
  document.getElementById('player-view').hidden = false;
  document.getElementById('player-title').textContent = movie.title;
  document.getElementById('player-year').textContent = movie.year || '';

  const video = document.getElementById('player-video');
  video.innerHTML = '';
  video.src = movie.videoUrl;
  if (movie.subtitleUrl) {
    const track = document.createElement('track');
    track.kind = 'subtitles';
    track.src = movie.subtitleUrl;
    track.srclang = 'en';
    track.label = 'English';
    track.default = true;
    video.appendChild(track);
  }
  video.load();
  video.play().catch(() => {});
  window.scrollTo(0, 0);
}

document.getElementById('back-btn').addEventListener('click', () => {
  const video = document.getElementById('player-video');
  video.pause();
  video.removeAttribute('src');
  video.innerHTML = '';
  document.getElementById('player-view').hidden = true;
  document.getElementById('grid-view').hidden = false;
});

document.getElementById('search').addEventListener('input', (e) => {
  query = e.target.value;
  page = 1;
  renderGrid();
});

document.querySelectorAll('input[name="sort"]').forEach((el) => {
  el.addEventListener('change', (e) => {
    sortBy = e.target.value;
    page = 1;
    renderGrid();
  });
});

document.getElementById('page-size').addEventListener('change', (e) => {
  pageSize = Number(e.target.value);
  page = 1;
  renderGrid();
});

document.getElementById('prev-page').addEventListener('click', () => { page -= 1; renderGrid(); });
document.getElementById('next-page').addEventListener('click', () => { page += 1; renderGrid(); });
pageInputEl.addEventListener('change', (e) => {
  page = Number(e.target.value) || 1;
  renderGrid();
});

renderGrid();
</script>
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

	movies, err := scanMovies()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	list := make([]movieJSON, 0, len(movies))
	for _, m := range movies {
		title, year := parseTitleYear(m.Name)
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
