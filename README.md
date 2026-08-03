# Movie Collection

A lightweight Go web server that plays your local MP4 movies and TV series in the browser.

## Requirements

- Go 1.22+

## Folder layout

Movies and series aren't stored in this project — point `MOVIES_DIR` and `SERIES_DIR` in `.env` (see [Configuration](#configuration)) at wherever your media actually lives.

Each movie gets its own subfolder, named the same as the movie — name it `Title (YYYY)` and the year shows up in the UI and can be sorted on. Drop a `.srt` file next to the `.mp4` (any name sharing the same prefix, e.g. `Iron Man (2008).eng.srt`) and it's shown as subtitles automatically. Drop a `poster.jpg`/`.png`/`.webp`/`.gif` (or any single image file) in the folder and it's used as the card artwork; otherwise a plain fallback is shown:

```
movies/
  Iron Man (2008)/
    Iron Man (2008).mp4
    Iron Man (2008).eng.srt
    poster.jpg
```

Put your series under a top-level `series/` folder. Each show gets its own subfolder, with a subfolder per season containing the episode files:

```
series/
  Breaking Bad/
    Season 1/
      S01E01 - Pilot.mp4
      S01E01 - Pilot.eng.srt
      S01E02 - Cat's in the Bag....mp4
    Season 2/
      S02E01 - Seven Thirty-Seven.mp4
```

Subtitles work the same way for episodes: a `.srt` file sharing the episode's filename prefix is picked up automatically.

Folder/file names are what's shown in the browser.

## Configuration

Copy `.env.template` to `.env` and adjust as needed:

```
cp .env.template .env
```

- `PORT` — port the server listens on (default `8080`)
- `MOVIES_DIR` — directory containing your movie subfolders (required; falls back to `movies` in the project root, which doesn't exist by default)
- `SERIES_DIR` — directory containing your series subfolders (required; falls back to `series` in the project root, which doesn't exist by default)
- `TMDB_API_KEY` — only needed for `make fetch-posters` (see below); get a free key at https://www.themoviedb.org/settings/api

## Fetching posters

Don't have `poster.*` files yet? `make fetch-posters` looks up each movie on TMDb by its folder name (`Title (YYYY)`) and downloads the matching poster into that folder:

```
make fetch-posters
```

It skips any movie that already has a poster, so it's safe to re-run after adding new movies. Movies TMDb can't match, or that don't parse a title/year from the folder name, are reported and left alone — add a `poster.jpg` manually for those.

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

## Generating subtitles

`.srt` files are converted to `.vtt` automatically the first time the server scans a movie/episode. To pre-generate them all instead (e.g. before starting the server, or just to check what's missing), run:

```
make generate-vtt
```

It skips anything that already has a `.vtt`, so it's safe to re-run after adding new movies or series.

## Playback

- **Keyboard shortcuts** (while a video is playing): `←`/`→` skip back/forward 10 seconds, `Shift+←`/`Shift+→` skip 30 seconds.
- **Resume where you left off**: playback position is saved (in a cookie, per movie/episode) every few seconds and on pause/exit, and restored automatically next time you open that title. Finishing a video clears its saved position.

## Running

From the project root:

```
make run
```

Then open http://localhost:8080 in your browser (or whatever `PORT` you set in `.env`).

## What it does

- `/` — a single-page movie browser: search by title, sort by year or name, paginate, click a card to play inline
- `/media/...` — serves raw movie video/poster/subtitle files, with support for seeking/scrubbing (HTTP range requests)
- `/refresh` — re-scans `MOVIES_DIR` and redirects back to `/` (see caching note below)

If a `.vtt` subtitle file isn't already present, a matching `.srt` is converted to WebVTT once and saved alongside it (same name, `.vtt` extension); later requests reuse that file instead of reconverting.

The movie list is scanned from disk once and cached in memory for the life of the server process — added/removed/renamed movies won't show up until you click "Refresh" in the nav bar (or restart the server).

Series support (`series/`, `SERIES_DIR`, `/series/...`, `/watch-series/...`) still works but isn't surfaced in the browser UI right now — movies are the focus.

## Status

Movies: searchable/sortable grid with posters, inline player, and `.srt` subtitles. Series: backend only, no UI yet.
