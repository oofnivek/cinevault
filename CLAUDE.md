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
