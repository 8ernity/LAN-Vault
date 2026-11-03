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

// joinPath joins a planned parent directory and a segment using the wire
// separator. The empty parent is treated as the destination root.
func joinPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

// dirNameResolver merges directory names case-insensitively (Windows profile)
// and keeps the first spelling encountered. Reserved directories are therefore
// known before any file collision suffix is assigned, regardless of manifest
// entry order.
type dirNameResolver struct {
	profile string
	names   map[string]string // osKey(parent) + "\x00" + osKey(segment) -> display
}

func newDirNameResolver(profile string) *dirNameResolver {
	return &dirNameResolver{profile: profile, names: make(map[string]string)}
}

func (r *dirNameResolver) resolve(parent, segment string) string {
	k := osKey(parent, r.profile) + "\x00" + osKey(segment, r.profile)
	if display, ok := r.names[k]; ok {
		return display
	}
	r.names[k] = segment
	return segment
}

// ResolveDirectoryPath resolves every directory segment of a wire path and
// returns the planned directory path. A file's final segment is included only
// when includeLast is true.
func (r *dirNameResolver) ResolveDirectoryPath(segments []string, includeLast bool) string {
	parent := ""
	limit := len(segments)
	if !includeLast && limit > 0 {
		limit--
	}
	for i := 0; i < limit; i++ {
		display := r.resolve(parent, segments[i])
		parent = joinPath(parent, display)
	}
	return parent
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

	// Raw (wire-level) file/directory conflicts. These are independent of the
	// destination profile and must be rejected before any filesystem work.
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

	// Normalize and sanitize every segment before planning. Directory names are
	// resolved first so the full directory namespace is reserved before any
	// file collision suffix can be assigned. This makes the decision
	// independent of manifest entry order (PROTOCOL 10.5).
	resolver := newDirNameResolver(profile)
	type entryPlan struct {
		entry        EntryInput
		segments     []string
		parent       string
		baseName     string
		resolvedName string
	}
	plans := make([]entryPlan, len(entries))
	for i, e := range entries {
		rawSegments := strings.Split(e.Path, "/")
		segments := make([]string, len(rawSegments))
		for j, s := range rawSegments {
			segments[j] = SanitizeSegment(s, profile)
			if len([]byte(segments[j])) > MaxSegmentBytes {
				return nil, "segment_too_long", nil
			}
		}
		plans[i] = entryPlan{entry: e, segments: segments}
	}

	reservedDirectories := make(map[string]bool)

	// Pass 1: resolve every explicit and implied directory.
	for _, p := range plans {
		includeLast := p.entry.Type == "dir"
		full := resolver.ResolveDirectoryPath(p.segments, includeLast)
		if full != "" {
			reservedDirectories[osKey(full, profile)] = true
		}
	}

	// Pass 2: reject sanitized/case-folded file-vs-directory conflicts wherever
	// the entries appear in the manifest.
	for i := range plans {
		p := &plans[i]
		if p.entry.Type != "file" {
			continue
		}
		p.parent = resolver.ResolveDirectoryPath(p.segments, false)
		p.baseName = p.segments[len(p.segments)-1]
		candidatePath := joinPath(p.parent, p.baseName)
		if reservedDirectories[osKey(candidatePath, profile)] {
			return nil, "file_dir_conflict", nil
		}
	}

	// Pass 3: assign collision-safe names to files. The reserved directory
	// namespace is authoritative for every candidate number.
	usedFiles := make(map[string]bool)
	for i := range plans {
		p := &plans[i]
		if p.entry.Type != "file" {
			continue
		}
		candidate := p.baseName
		for n := 0; ; n++ {
			if n > 0 {
				candidate = numberedName(p.baseName, n)
			}
			if len([]byte(candidate)) > MaxSegmentBytes {
				return nil, "segment_too_long", nil
			}
			path := joinPath(p.parent, candidate)
			if len([]byte(path)) > MaxPathBytes {
				return nil, "path_too_long", nil
			}
			key := osKey(path, profile)
			if !usedFiles[key] && !reservedDirectories[key] {
				usedFiles[key] = true
				p.resolvedName = path
				break
			}
		}
	}

	result := make([]string, 0, len(plans))
	for i := range plans {
		p := &plans[i]
		if p.entry.Type == "dir" {
			result = append(result, resolver.ResolveDirectoryPath(p.segments, true))
		} else {
			result = append(result, p.resolvedName)
		}
	}

	for _, path := range result {
		if len([]byte(path)) > MaxPathBytes {
			return nil, "path_too_long", nil
		}
		for _, s := range strings.Split(path, "/") {
			if len([]byte(s)) > MaxSegmentBytes {
				return nil, "segment_too_long", nil
			}
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

