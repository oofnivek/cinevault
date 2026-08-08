# CLAUDE.md

Guidance for Claude Code when working in this repo.

## TMDb metadata convention

When saving a TMDb API response into a movie's own folder, store it as
`tmdb.json` containing the single matched movie **object**, not the raw
search-endpoint wrapper. i.e. write `results[0]`, not `{page, results,
total_pages, total_results}` — the pagination/search-shape fields are
meaningless once a specific result has been picked for a specific folder;
one movie folder maps to one flattened object.

Shared reference data that isn't tied to any single movie (e.g. TMDb's
genre id→name list) lives at the repo root instead, as `tmdb_genres.json`
— unwrapped from its `{"genres": [...]}` envelope but otherwise kept in
TMDb's own array-of-`{id, name}` shape, since it's a lookup table rather
than a single-object record.

## Pinning an ambiguous TMDb match

`cmd/fetch-posters` matches a movie folder to TMDb via a title/year text
search, which can pick the wrong result for short or generic titles. To
force a specific match, drop a `tmdb_id.txt` file in the movie's folder
containing just the numeric TMDb movie ID (found in a TMDb URL, e.g.
`themoviedb.org/movie/603692` → `603692`). When present, `fetch-posters`
fetches `/movie/{id}` directly instead of searching.

## TMDb metadata for series

`cmd/fetch-series-posters` works the same way as `cmd/fetch-posters`, but
against TMDb's `/search/tv` and `/tv/{id}` endpoints, matched by series
folder name (no year — series folders aren't named with one) and saved as
`tmdb.json` at the series' own folder root. The `tmdb_id.txt` pinning
convention below works the same way for series folders as it does for
movies.

TMDb also gives each *season* its own poster, distinct from the show-level
one, via `/tv/{series_id}/season/{season_number}`. `fetch-series-posters`
fetches that too, saving it as `tmdb.json`/`poster.*` inside that season's
own folder (e.g. `Breaking Bad/S01/tmdb.json`) — same flattened-object
convention, just one level down. That endpoint's own top-level fields are
what's saved; the JSON's `episodes` sub-list is deliberately left out of
the saved `tmdb.json` since episode number/title data comes from the
folder scan (parsed from filenames), not from TMDb.

That same `episodes` sub-list is still *used*, just not persisted as JSON:
each entry's `still_path` (TMDb's term for an episode thumbnail) is
downloaded and saved next to that episode's video file, matched by episode
number and named after it — e.g. `Breaking Bad - s01e01.jpg` alongside
`Breaking Bad - s01e01.mp4`. One image file is the convention here (unlike
the movie/series/season level, there's no `tmdb.json` per episode).

## Series folder convention

`SERIES_DIR` (env var, default `series`) holds one subfolder per series,
named after the series (e.g. `Breaking Bad`). Inside that, one subfolder
per season named `S01`, `S02`, etc. Inside each season folder, episode
files are named `<series> - sNNeNN.mp4` or `<series> - sNNeNN - <episode
title>.mp4`, e.g. `Breaking Bad - s01e01.mp4` or `Breaking Bad - s01e01 -
Pilot.mp4` — the `sNNeNN` marker (case-insensitive) is what's parsed for
season/episode numbers; everything after it up to the extension, if
present, is taken as the episode title.
