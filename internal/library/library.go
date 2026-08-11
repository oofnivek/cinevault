// Package library holds movie- and series-folder conventions shared by the
// web server and the poster-fetching tool: how a poster image is
// recognized, how a folder name like "Iron Man (2008)" splits into title
// and year, and how series/season/episode folders and filenames parse.
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

// removePosterFiles deletes any "poster.<ext>" file in dir, for every
// extension FindPoster recognizes. Used before a forced re-fetch downloads
// a fresh poster, so a format change (e.g. TMDb now serving a .jpg where
// it used to be a .png) doesn't leave the stale file sitting alongside the
// new one — FindPoster would then be picking between two candidates.
func removePosterFiles(dir string) {
	for ext := range posterExts {
		os.Remove(filepath.Join(dir, "poster"+ext))
	}
}

// FindStill returns the name of an episode still image file in dir that
// shares mp4Name's base name (e.g. "Breaking Bad - s01e01.jpg" alongside
// "Breaking Bad - s01e01.mp4"), or "" if none is found.
func FindStill(dir, mp4Name string) string {
	files, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	base := strings.TrimSuffix(mp4Name, filepath.Ext(mp4Name))
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		ext := filepath.Ext(f.Name())
		if !posterExts[strings.ToLower(ext)] {
			continue
		}
		if strings.TrimSuffix(f.Name(), ext) == base {
			return f.Name()
		}
	}
	return ""
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

var seasonDirPattern = regexp.MustCompile(`(?i)^S(\d+)$`)

// ParseSeasonDir returns the season number encoded in a season folder name
// like "S01", or ok=false if name doesn't match that pattern.
func ParseSeasonDir(name string) (season int, ok bool) {
	m := seasonDirPattern.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

var episodeFilePattern = regexp.MustCompile(`(?i)s(\d+)e(\d+)`)

// ParseEpisodeFile extracts the season and episode numbers, and optional
// episode title, from a filename like "Breaking Bad - s01e01.mp4" or
// "Breaking Bad - s01e01 - Pilot.mp4". ok is false if no "sNNeNN" marker is
// found.
func ParseEpisodeFile(name string) (season, episode int, title string, ok bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	loc := episodeFilePattern.FindStringSubmatchIndex(base)
	if loc == nil {
		return 0, 0, "", false
	}
	s, err1 := strconv.Atoi(base[loc[2]:loc[3]])
	e, err2 := strconv.Atoi(base[loc[4]:loc[5]])
	if err1 != nil || err2 != nil {
		return 0, 0, "", false
	}
	title = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(base[loc[1]:]), "-"))
	return s, e, title, true
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
