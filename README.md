# Movie Collection

A lightweight Go web server that plays your local MP4 movies and TV series in the browser.

## Requirements

- Go 1.22+

## Folder layout

Movies and series aren't stored in this project — point `MOVIES_DIR` and `SERIES_DIR` in `.env` (see [Configuration](#configuration)) at wherever your media actually lives.

Each movie gets its own subfolder, named the same as the movie. Drop a `.srt` file next to the `.mp4` (any name sharing the same prefix, e.g. `Iron Man (2008).eng.srt`) and it's shown as subtitles automatically:

```
movies/
  Iron Man (2008)/
    Iron Man (2008).mp4
    Iron Man (2008).eng.srt
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

## Running

From the project root:

```
make run
```

Then open http://localhost:8080 in your browser (or whatever `PORT` you set in `.env`).

## What it does

- `/` — lists all movies found under `MOVIES_DIR` and all series found under `SERIES_DIR`
- `/watch/<name>` — plays a movie in an HTML5 video player
- `/media/...` — serves raw movie video files, with support for seeking/scrubbing (HTTP range requests)
- `/series/<show>` — lists a show's seasons
- `/series/<show>/<season>` — lists a season's episodes
- `/watch-series/<show>/<season>/<episode>` — plays an episode in an HTML5 video player
- `/series-media/...` — serves raw episode video files, with support for seeking/scrubbing (HTTP range requests)

If a `.vtt` subtitle file isn't already present, a matching `.srt` is converted to WebVTT once and saved alongside it (same name, `.vtt` extension); later requests reuse that file instead of reconverting.

## Status

Currently plays MP4s with `.srt` subtitles. Automatic poster thumbnails are not wired up yet.
