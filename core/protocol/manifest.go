package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrOversizedEntry   = errors.New("single manifest entry exceeds control frame limit")
	ErrNonSequentialIdx = errors.New("manifest entries indices are not sequential starting from 0")
	ErrDuplicateIndex   = errors.New("duplicate manifest index")
)

// FormatCanonicalManifestLine produces "<type>\t<path>\t<size>\t<mtime_ms>\n"
func FormatCanonicalManifestLine(entry ManifestEntry) string {
	return fmt.Sprintf("%s\t%s\t%d\t%d\n", entry.Type, entry.Path, entry.Size, entry.MtimeMs)
}

// ComputeManifestHash computes SHA-256 over canonical lines of all entries in index order
func ComputeManifestHash(entries []ManifestEntry) string {
	h := sha256.New()
	for _, e := range entries {
		line := FormatCanonicalManifestLine(e)
		h.Write([]byte(line))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// EncodedPartFrameSize calculates 1 (type byte) + serialized JSON length
func EncodedPartFrameSize(entries []ManifestEntry) (int, error) {
	msg := ManifestPartMessage{Entries: entries}
	data, err := json.Marshal(msg)
	if err != nil {
		return 0, err
	}
	return 1 + len(data), nil
}

// BatchManifest splits manifest entries into parts respecting both entry count and byte cap
func BatchManifest(entries []ManifestEntry, maxEntries int, maxFrameBytes int) ([][]ManifestEntry, error) {
	emptySize, err := EncodedPartFrameSize([]ManifestEntry{})
	if err != nil {
		return nil, err
	}
	if maxEntries <= 0 || maxFrameBytes <= emptySize {
		return nil, errors.New("invalid frame or entry limits")
	}

	var parts [][]ManifestEntry
	var current []ManifestEntry

	for _, entry := range entries {
		singleSize, err := EncodedPartFrameSize([]ManifestEntry{entry})
		if err != nil {
			return nil, err
		}
		if singleSize > maxFrameBytes {
			return nil, fmt.Errorf("%w: path %s requires %d bytes", ErrOversizedEntry, entry.Path, singleSize)
		}

		if len(current) > 0 {
			if len(current) >= maxEntries {
				parts = append(parts, current)
				current = nil
			} else {
				testBatch := append(current, entry)
				newSize, err := EncodedPartFrameSize(testBatch)
				if err != nil {
					return nil, err
				}
				if newSize > maxFrameBytes {
					parts = append(parts, current)
					current = nil
				}
			}
		}

		current = append(current, entry)
	}

	if len(current) > 0 {
		parts = append(parts, current)
	}

	return parts, nil
}

// ValidateManifestIndices ensures all entries have strictly sequential indices starting from 0
func ValidateManifestIndices(entries []ManifestEntry) error {
	for i, e := range entries {
		if e.Index != i {
			return fmt.Errorf("%w: expected index %d, got %d", ErrNonSequentialIdx, i, e.Index)
		}
	}
	return nil
}
