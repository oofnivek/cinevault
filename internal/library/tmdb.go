// Helpers shared by cmd/fetch-posters and cmd/fetch-series-posters: reading
// a manually pinned TMDb ID, and saving the JSON/poster TMDb returns.
package library

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FileExists reports whether path exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ReadTMDbID reads a manually pinned TMDb ID from tmdb_id.txt in dir, used
// to disambiguate short/generic titles that TMDb search matches poorly (see
// CLAUDE.md). ok is false if the file doesn't exist or isn't a plain
// integer.
func ReadTMDbID(dir string) (id int, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, "tmdb_id.txt"))
	if err != nil {
		return 0, false
	}
	id, err = strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return id, true
}

// SaveJSON marshals v as indented JSON and writes it to path.
func SaveJSON(v any, path string) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// DownloadPoster downloads a TMDb poster image, given the poster_path a
// search/detail response returned (e.g. "/abc123.jpg"), into dir as
// "poster<ext>".
func DownloadPoster(posterPath, dir string) error {
	posterURL := "https://image.tmdb.org/t/p/w500" + posterPath
	resp, err := http.Get(posterURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}

	ext := filepath.Ext(posterPath)
	if ext == "" {
		ext = ".jpg"
	}
	out, err := os.Create(filepath.Join(dir, "poster"+ext))
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}
