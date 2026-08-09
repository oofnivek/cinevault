# Movie Collection

A lightweight Go web server that plays your local MP4 movies in the browser.

## Requirements

- Go 1.22+

## Folder layout

Movies aren't stored in this project — point `MOVIES_DIR` in `.env` (see [Configuration](#configuration)) at wherever your media actually lives.

Each movie gets its own subfolder, named the same as the movie — name it `Title (YYYY)` and the year shows up in the UI and can be sorted on. Drop a `.srt` file next to the `.mp4` (any name sharing the same prefix, e.g. `Iron Man (2008).eng.srt`) and it's shown as subtitles automatically. Drop a `poster.jpg`/`.png`/`.webp`/`.gif` (or any single image file) in the folder and it's used as the card artwork; otherwise a plain fallback is shown:

```
movies/
  Iron Man (2008)/
    Iron Man (2008).mp4
    Iron Man (2008).eng.srt
    poster.jpg
```

Folder/file names are what's shown in the browser.

### Series

Series live in a separate tree — point `SERIES_DIR` at it. Each series gets its own subfolder, named after the series (no year), containing one subfolder per season named `S01`, `S02`, etc. Episode files inside a season folder are named `<series> - sNNeNN.mp4` or `<series> - sNNeNN - <episode title>.mp4`; a `poster.*` dropped in the series folder is used as its card artwork, same as movies:

```
series/
  Breaking Bad/
    poster.jpg
    S01/
      Breaking Bad - s01e01.mp4
      Breaking Bad - s01e01 - Pilot.mp4
```

## Configuration

Copy `.env.template` to `.env` and adjust as needed:

```
cp .env.template .env
```

- `PORT` — port the server listens on (default `8080`)
- `MOVIES_DIR` — directory containing your movie subfolders (required; falls back to `movies` in the project root, which doesn't exist by default)
- `SERIES_DIR` — directory containing your series subfolders (falls back to `series` in the project root, which doesn't exist by default)
- `TMDB_API_KEY` — only needed for `make fetch-posters`/`make fetch-series-posters` (see below); get a free key at https://www.themoviedb.org/settings/api

## Fetching posters

Don't have `poster.*` files yet? `make fetch-posters` looks up each movie on TMDb by its folder name (`Title (YYYY)`) and downloads the matching poster into that folder:

```
make fetch-posters
```

It skips any movie that already has a poster, so it's safe to re-run after adding new movies. Movies TMDb can't match, or that don't parse a title/year from the folder name, are reported and left alone — add a `poster.jpg` manually for those.

### Fetching series posters

`make fetch-series-posters` does the same thing for `SERIES_DIR`, matching each series folder to TMDb by name:

```
make fetch-series-posters
```

It fetches metadata/posters at three levels in one pass: the series itself, each season (TMDb gives every season its own poster, distinct from the show-level one), and each episode's thumbnail (downloaded alongside its video file, e.g. `Breaking Bad - s01e01.jpg` next to `Breaking Bad - s01e01.mp4`). It's safe to re-run — anything already fetched is skipped.

## Looking up the correct name for a movie

If a folder is named wrong (typo, wrong year, wrong title entirely) so `fetch-posters` couldn't match it — or matched it to the wrong movie — look up the correct title/year and poster with `scripts/lookup-movie.py`. It's a standalone Python script (stdlib only, no `pip install` needed) that searches TMDb and prints candidate matches with the folder name to rename to and a poster URL; it doesn't touch any files itself:

```
make lookup-movie NAME="rough title or folder name"
```

or run it directly:

```
python3 scripts/lookup-movie.py "rough title or folder name"
```

It reuses `TMDB_API_KEY` from `.env`, same as `fetch-posters`. Rename the folder and drop in the poster yourself once you've found the right match. Requires Python 3.

For a series matched wrong (or not at all) by `fetch-series-posters`, drop a `tmdb_id.txt` file in the series folder (or a season folder) containing the numeric TMDb ID from a TMDb URL, e.g. `themoviedb.org/tv/1396` → `1396` — `fetch-series-posters` then fetches that ID directly instead of searching by name.

## Generating subtitles

`.srt` files are converted to `.vtt` automatically the first time the server scans a movie/episode. To pre-generate them all instead (e.g. before starting the server, or just to check what's missing), run:

```
make generate-vtt
```

It skips anything that already has a `.vtt`, so it's safe to re-run after adding new movies.

## Playback

- **Keyboard shortcuts** (while a video is playing): `←`/`→` skip back/forward 10 seconds, `Shift+←`/`Shift+→` skip 30 seconds, `↑`/`↓` volume up/down.
- **Resume where you left off**: playback position is saved (in a cookie, per movie/episode) every few seconds and on pause/exit, and restored automatically next time you open that title. Finishing a video clears its saved position.

## Running

From the project root:

```
make run
```

Then open http://localhost:8080 in your browser (or whatever `PORT` you set in `.env`).

## What it does

- `/` — a single-page movie browser: search by title/genre, sort by year or name, paginate, click a card to play
- `/media/...` — serves raw movie video/poster/subtitle files, with support for seeking/scrubbing (HTTP range requests)
- `/series` — a grid of series, click a card to see its seasons
- `/series/{name}` — season tiles for one series; `/series/{name}/{season dir}` — that season's episode list
- `/watch-series/...` — plays a single episode
- `/series-media/...` — serves raw series/season/episode video/poster/still/subtitle files, with support for seeking/scrubbing (HTTP range requests)

If a `.vtt` subtitle file isn't already present, a matching `.srt` is converted to WebVTT once and saved alongside it (same name, `.vtt` extension); later requests reuse that file instead of reconverting.

The movie and series lists are each scanned from disk once and cached in memory for the life of the server process — added/removed/renamed movies or series won't show up until you restart the server (`make run`).

## Status

Movies: searchable/sortable grid with posters, inline player, and `.srt` subtitles.
Series: browsable series → season → episode grid with posters/stills, inline player, and `.srt` subtitles.
