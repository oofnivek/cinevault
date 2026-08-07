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

var srtTimestamp = regexp.MustCompile(`(\d{2}:\d{2}:\d{2}),(\d{3})`)

// srtToVTT converts SRT subtitle content to WebVTT, the only format
// natively supported by HTML5 <track> elements.
func srtToVTT(srt []byte) []byte {
	body := strings.TrimPrefix(string(srt), "\ufeff")
	body = srtTimestamp.ReplaceAllString(body, "$1.$2")
	body = strings.ReplaceAll(body, `{\an8}`, "")
	return []byte("WEBVTT\n\n" + body)
}

// EnsureSubtitle looks in dir for a .vtt file matching mp4Name's prefix and
// returns its name if found, along with any matching .srt file's name.
// Otherwise, if only a matching .srt file exists, it converts it to .vtt
// once, writes it alongside the .srt, and returns both names. vttName is ""
// if no subtitle is available in either format.
func EnsureSubtitle(dir, mp4Name string) (vttName, srtName string) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return "", ""
	}
	base := strings.TrimSuffix(mp4Name, filepath.Ext(mp4Name))

	for _, f := range files {
		if f.IsDir() || !strings.HasPrefix(f.Name(), base) {
			continue
		}
		switch strings.ToLower(filepath.Ext(f.Name())) {
		case ".vtt":
			vttName = f.Name()
		case ".srt":
			srtName = f.Name()
		}
	}
	if vttName != "" || srtName == "" {
		return vttName, srtName
	}

	srtData, err := os.ReadFile(filepath.Join(dir, srtName))
	if err != nil {
		return "", srtName
	}
	vttName = strings.TrimSuffix(srtName, filepath.Ext(srtName)) + ".vtt"
	if err := os.WriteFile(filepath.Join(dir, vttName), srtToVTT(srtData), 0644); err != nil {
		return "", srtName
	}
	return vttName, srtName
}
