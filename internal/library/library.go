// Package library holds movie-folder conventions shared by the web server
// and the poster-fetching tool: how a poster image is recognized, and how
// a folder name like "Iron Man (2008)" splits into title and year.
package library

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var posterExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}

// FindPoster returns the name of an image file in dir to use as a poster,
// preferring one named "poster.*", otherwise the first image file found.
// Returns "" if none is found.
func FindPoster(dir string) string {
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

// ParseTitleYear splits a folder name like "Iron Man (2008)" into its title
// and year. If name doesn't end in "(YYYY)", year is 0.
func ParseTitleYear(name string) (title string, year int) {
	m := yearPattern.FindStringSubmatch(name)
	if m == nil {
		return name, 0
	}
	y, _ := strconv.Atoi(m[2])
	return m[1], y
}
