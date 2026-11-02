package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const (
	MaxPathBytes    = 4096
	MaxSegmentBytes = 255
)

var (
	driveRegex = regexp.MustCompile(`^[A-Za-z]:$`)
	winBadChars = `<>:"|?*`
	reservedNames = map[string]bool{
		"CON": true, "PRN": true, "AUX": true, "NUL": true,
		"CONIN$": true, "CONOUT$": true,
		"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
		"COM6": true, "COM7": true, "COM8": true, "COM9": true,
		"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
		"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	}
)

func isBidi(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

// ValidatePath checks wire path validity and returns reason if invalid, or "" if valid
func ValidatePath(p string) string {
	if p == "" {
		return "empty"
	}
	if strings.HasPrefix(p, "/") {
		return "absolute"
	}
	if strings.Contains(p, "\\") {
		return "backslash"
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7F {
			return "control"
		}
	}

	segs := strings.Split(p, "/")
	for _, s := range segs {
		if s == "" {
			return "empty_segment"
		}
		if s == "." || s == ".." {
			return "dot_segment"
		}
	}

	if driveRegex.MatchString(segs[0]) {
		return "drive_letter"
	}

	for _, s := range segs {
		if len([]byte(s)) > MaxSegmentBytes {
			return "segment_too_long"
		}
	}
	if len([]byte(p)) > MaxPathBytes {
		return "path_too_long"
	}

	return ""
}

// SanitizeSegment normalizes and sanitizes a single path segment for the target OS profile
func SanitizeSegment(seg string, profile string) string {
	seg = norm.NFC.String(seg)
	if profile != "windows" {
		return seg
	}

	var b strings.Builder
	for _, r := range seg {
		if strings.ContainsRune(winBadChars, r) || r < 0x20 {
			b.WriteByte('_')
		} else if isBidi(r) {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	s := b.String()

	s = strings.TrimRight(s, " .")
	if s == "" {
		s = "_"
	}

	stem := strings.ToUpper(strings.TrimRight(strings.Split(s, ".")[0], " "))
	if reservedNames[stem] {
		s = "_" + s
	}

	return s
}

func osKey(s string, profile string) string {
	if profile == "windows" {
		return strings.ToLower(s)
	}
	return s
}

func numberedName(name string, n int) string {
	i := strings.LastIndex(name, ".")
	if i <= 0 {
		return fmt.Sprintf("%s (%d)", name, n)
	}
	return fmt.Sprintf("%s (%d)%s", name[:i], n, name[i:])
}

// EntryInput represents an incoming entry in manifest planning
type EntryInput struct {
	Type string // "file" or "dir"
	Path string
}

type preparedEntry struct {
	isDir     bool
	parent    string
	basename  string
	directory string
}

type dirKey struct {
	parent string
	segKey string
}

// PlanManifest plans all destinations before writing, resolving collisions and enforcing security rules
func PlanManifest(entries []EntryInput, profile string) (paths []string, rejectReason string, err error) {
	if profile != "windows" && profile != "posix" {
		return nil, "", errors.New("unknown destination profile")
	}

	for _, e := range entries {
		if e.Type != "file" && e.Type != "dir" {
			return nil, "invalid_entry_type", nil
		}
		why := ValidatePath(e.Path)
		if why != "" {
			return nil, why, nil
		}
	}

	seen := make(map[string]bool)
	for _, e := range entries {
		k := e.Type + "\t" + e.Path
		if seen[k] {
			return nil, "duplicate_path", nil
		}
		seen[k] = true
	}

	files := make(map[string]bool)
	dirs := make(map[string]bool)
	parents := make(map[string]bool)

	for _, e := range entries {
		if e.Type == "file" {
			files[e.Path] = true
		} else {
			dirs[e.Path] = true
		}
		parts := strings.Split(e.Path, "/")
		for i := 1; i < len(parts); i++ {
			parents[strings.Join(parts[:i], "/")] = true
		}
	}

	for f := range files {
		if dirs[f] || parents[f] {
			return nil, "file_dir_conflict", nil
		}
	}

	directoryNames := make(map[dirKey]string)
	reservedDirectories := make(map[string]bool)
	var prepared []preparedEntry

	for _, e := range entries {
		segments := strings.Split(e.Path, "/")
		sanitizedSegments := make([]string, len(segments))
		for i, s := range segments {
			sanitizedSegments[i] = SanitizeSegment(s, profile)
		}

		parent := ""
		for i, s := range sanitizedSegments {
			isDir := i < len(sanitizedSegments)-1 || e.Type == "dir"
			if isDir {
				k := dirKey{parent: parent, segKey: osKey(s, profile)}
				display, exists := directoryNames[k]
				if !exists {
					display = s
					directoryNames[k] = s
				}
				var path string
				if parent != "" {
					path = parent + "/" + display
				} else {
					path = display
				}
				reservedDirectories[osKey(path, profile)] = true
				parent = path
			} else {
				var targetPath string
				if parent != "" {
					targetPath = parent + "/" + s
				} else {
					targetPath = s
				}
				if reservedDirectories[osKey(targetPath, profile)] {
					return nil, "file_dir_conflict", nil
				}
				prepared = append(prepared, preparedEntry{
					isDir:    false,
					parent:   parent,
					basename: s,
				})
			}
		}
		if e.Type == "dir" {
			prepared = append(prepared, preparedEntry{
				isDir:     true,
				directory: parent,
			})
		}
	}

	usedFiles := make(map[string]bool)
	var result []string

	for _, p := range prepared {
		if p.isDir {
			result = append(result, p.directory)
			continue
		}

		var basePath string
		if p.parent != "" {
			basePath = p.parent + "/" + p.basename
		} else {
			basePath = p.basename
		}
		if reservedDirectories[osKey(basePath, profile)] {
			return nil, "file_dir_conflict", nil
		}

		candidate := p.basename
		n := 0
		var path string
		var key string

		for {
			if p.parent != "" {
				path = p.parent + "/" + candidate
			} else {
				path = candidate
			}
			key = osKey(path, profile)
			if !usedFiles[key] && !reservedDirectories[key] {
				break
			}
			n++
			candidate = numberedName(p.basename, n)
			if len([]byte(candidate)) > MaxSegmentBytes {
				return nil, "segment_too_long", nil
			}
		}

		if len([]byte(path)) > MaxPathBytes {
			return nil, "path_too_long", nil
		}
		usedFiles[key] = true
		result = append(result, path)
	}

	for _, path := range result {
		for _, s := range strings.Split(path, "/") {
			if len([]byte(s)) > MaxSegmentBytes {
				return nil, "segment_too_long", nil
			}
		}
		if len([]byte(path)) > MaxPathBytes {
			return nil, "path_too_long", nil
		}
	}

	return result, "", nil
}

// PlanSingle convenience function for a single file path
func PlanSingle(path string, profile string) (finalPath string, rejectReason string, err error) {
	paths, reject, err := PlanManifest([]EntryInput{{Type: "file", Path: path}}, profile)
	if err != nil || reject != "" {
		return "", reject, err
	}
	if len(paths) > 0 {
		return paths[0], "", nil
	}
	return "", "", nil
}

// AvailableDiskFilename returns fullPath if it does not exist on disk, or appends " (1)", " (2)", etc. before the extension
func AvailableDiskFilename(fullPath string) string {
	if _, err := os.Lstat(fullPath); errors.Is(err, os.ErrNotExist) {
		return fullPath
	}
	dir := filepath.Dir(fullPath)
	base := filepath.Base(fullPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	for i := 1; i < 10000; i++ {
		candName := fmt.Sprintf("%s (%d)%s", name, i, ext)
		candPath := filepath.Join(dir, candName)
		if _, err := os.Lstat(candPath); errors.Is(err, os.ErrNotExist) {
			return candPath
		}
	}
	return fullPath
}

