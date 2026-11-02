package transfer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"lanvault/core/fsutil"
	"lanvault/core/protocol"
)

// ScannedItem represents a source file to be sent
type ScannedItem struct {
	SourceDiskPath string
	Entry          protocol.ManifestEntry
}

// ScanSourceItems scans a list of files and folders on disk to build manifest entries
func ScanSourceItems(itemPaths []string) ([]ScannedItem, int, int64, string, error) {
	var items []ScannedItem
	var manifestEntries []protocol.ManifestEntry
	seenPaths := make(map[string]bool)

	currentIndex := 0

	for _, p := range itemPaths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, 0, 0, "", fmt.Errorf("failed to stat %s: %w", p, err)
		}

		if !info.IsDir() {
			wirePath := filepath.Base(p)
			if seenPaths[wirePath] {
				continue
			}
			seenPaths[wirePath] = true

			entry := protocol.ManifestEntry{
				Index:   currentIndex,
				Type:    "file",
				Path:    wirePath,
				Size:    info.Size(),
				MtimeMs: info.ModTime().UnixMilli(),
			}
			items = append(items, ScannedItem{
				SourceDiskPath: p,
				Entry:          entry,
			})
			manifestEntries = append(manifestEntries, entry)
			currentIndex++
		} else {
			// Directory: walk recursively
			baseParent := filepath.Dir(p)
			err := filepath.WalkDir(p, func(currPath string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				rel, err := filepath.Rel(baseParent, currPath)
				if err != nil {
					return err
				}
				wirePath := filepath.ToSlash(rel)
				if wirePath == "." || wirePath == "" {
					return nil
				}
				if seenPaths[wirePath] {
					return nil
				}
				seenPaths[wirePath] = true

				dInfo, err := d.Info()
				if err != nil {
					return err
				}

				entryType := "file"
				var size int64 = 0
				if d.IsDir() {
					entryType = "dir"
				} else {
					size = dInfo.Size()
				}

				entry := protocol.ManifestEntry{
					Index:   currentIndex,
					Type:    entryType,
					Path:    wirePath,
					Size:    size,
					MtimeMs: dInfo.ModTime().UnixMilli(),
				}
				items = append(items, ScannedItem{
					SourceDiskPath: currPath,
					Entry:          entry,
				})
				manifestEntries = append(manifestEntries, entry)
				currentIndex++
				return nil
			})
			if err != nil {
				return nil, 0, 0, "", fmt.Errorf("failed to walk folder %s: %w", p, err)
			}
		}
	}

	// Validate path security and limits before offering
	inputs := make([]fsutil.EntryInput, len(manifestEntries))
	for i, e := range manifestEntries {
		inputs[i] = fsutil.EntryInput{Type: e.Type, Path: e.Path}
	}
	// Pre-validate that all paths are syntactically valid on the wire
	for _, in := range inputs {
		if reason := fsutil.ValidatePath(in.Path); reason != "" {
			return nil, 0, 0, "", fmt.Errorf("invalid path %q in selection: %s", in.Path, reason)
		}
	}

	totalFiles := 0
	var totalBytes int64 = 0
	for _, e := range manifestEntries {
		if e.Type == "file" {
			totalFiles++
			totalBytes += e.Size
		}
	}

	manifestHash := protocol.ComputeManifestHash(manifestEntries)
	return items, totalFiles, totalBytes, manifestHash, nil
}
