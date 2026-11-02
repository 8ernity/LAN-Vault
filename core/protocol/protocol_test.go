package protocol

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func findProjectRoot(t *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "docs", "protocol-constants.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find project root from %s", wd)
		}
		dir = parent
	}
}

func TestWireFramesVector(t *testing.T) {
	root := findProjectRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testvectors", "frames.json"))
	if err != nil {
		t.Fatalf("read frames.json failed: %v", err)
	}

	var data struct {
		Frames []struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			PayloadHex  string `json:"payload_hex"`
			FrameHex    string `json:"frame_hex"`
			FileIndex   *int   `json:"file_index"`
			ChunkIndex  *int   `json:"chunk_index"`
			ChunkHex    string `json:"chunk_hex"`
			ChunkSHA256 string `json:"chunk_sha256"`
			Pairs       [][]int `json:"pairs"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal frames.json failed: %v", err)
	}

	for _, f := range data.Frames {
		t.Run(f.Name, func(t *testing.T) {
			expectedFrameBytes, err := hex.DecodeString(f.FrameHex)
			if err != nil {
				t.Fatalf("decode frame_hex failed: %v", err)
			}

			// Test ReadFrame
			frame, err := ReadFrame(bytes.NewReader(expectedFrameBytes))
			if err != nil {
				t.Fatalf("ReadFrame failed: %v", err)
			}

			if frame.Type.String() != f.Type {
				t.Errorf("frame type mismatch: expected %s, got %s", f.Type, frame.Type.String())
			}

			// Test writing frame back matches expected bytes
			var out bytes.Buffer
			if err := WriteRawFrame(&out, frame.Type, frame.Payload); err != nil {
				t.Fatalf("WriteRawFrame failed: %v", err)
			}
			if !bytes.Equal(out.Bytes(), expectedFrameBytes) {
				t.Errorf("WriteRawFrame output mismatch:\ngot:  %x\nwant: %x", out.Bytes(), expectedFrameBytes)
			}

			// Frame specific checks
			switch frame.Type {
			case FrameData:
				df, err := DecodeDataFramePayload(frame.Payload)
				if err != nil {
					t.Fatalf("DecodeDataFramePayload failed: %v", err)
				}
				if int(df.FileIndex) != *f.FileIndex {
					t.Errorf("file index mismatch: got %d, want %d", df.FileIndex, *f.FileIndex)
				}
				if int(df.ChunkIndex) != *f.ChunkIndex {
					t.Errorf("chunk index mismatch: got %d, want %d", df.ChunkIndex, *f.ChunkIndex)
				}
				if hex.EncodeToString(df.Data) != f.ChunkHex {
					t.Errorf("chunk hex mismatch: got %s, want %s", hex.EncodeToString(df.Data), f.ChunkHex)
				}
				if hex.EncodeToString(df.Hash[:]) != f.ChunkSHA256 {
					t.Errorf("chunk sha256 mismatch: got %s, want %s", hex.EncodeToString(df.Hash[:]), f.ChunkSHA256)
				}
			case FrameChunkAck, FrameChunkNeed:
				pairs, err := DecodeIndexPairs(frame.Payload)
				if err != nil {
					t.Fatalf("DecodeIndexPairs failed: %v", err)
				}
				if len(pairs) != len(f.Pairs) {
					t.Fatalf("pairs length mismatch: got %d, want %d", len(pairs), len(f.Pairs))
				}
				for i, p := range pairs {
					if int(p.FileIndex) != f.Pairs[i][0] || int(p.ChunkIndex) != f.Pairs[i][1] {
						t.Errorf("pair %d mismatch: got (%d,%d), want (%d,%d)", i, p.FileIndex, p.ChunkIndex, f.Pairs[i][0], f.Pairs[i][1])
					}
				}
			}
		})
	}
}

func TestBitmapVector(t *testing.T) {
	root := findProjectRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testvectors", "bitmap.json"))
	if err != nil {
		t.Fatalf("read bitmap.json failed: %v", err)
	}

	var data struct {
		ChunkCount      int    `json:"chunk_count"`
		Completed       []int  `json:"completed"`
		BitmapHex       string `json:"bitmap_hex"`
		BitmapBase64    string `json:"bitmap_base64"`
		InvalidExamples []struct {
			Note         string `json:"note"`
			ChunkCount   int    `json:"chunk_count"`
			BitmapBase64 string `json:"bitmap_base64"`
		} `json:"invalid_examples"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal bitmap.json failed: %v", err)
	}

	// 1. Build from completed list and verify hex and base64
	bmp := NewBitmap(data.ChunkCount)
	for _, idx := range data.Completed {
		if err := bmp.Set(idx); err != nil {
			t.Fatalf("Set(%d) failed: %v", idx, err)
		}
	}

	if bmp.ToHex() != data.BitmapHex {
		t.Errorf("Bitmap hex mismatch: got %s, want %s", bmp.ToHex(), data.BitmapHex)
	}
	if bmp.ToBase64() != data.BitmapBase64 {
		t.Errorf("Bitmap base64 mismatch: got %s, want %s", bmp.ToBase64(), data.BitmapBase64)
	}

	// 2. Parse from hex and base64
	fromH, err := FromHex(data.ChunkCount, data.BitmapHex)
	if err != nil {
		t.Fatalf("FromHex failed: %v", err)
	}
	if fromH.ToBase64() != data.BitmapBase64 {
		t.Errorf("FromHex -> ToBase64 mismatch: got %s, want %s", fromH.ToBase64(), data.BitmapBase64)
	}

	fromB64, err := FromBase64(data.ChunkCount, data.BitmapBase64)
	if err != nil {
		t.Fatalf("FromBase64 failed: %v", err)
	}
	for _, idx := range data.Completed {
		if !fromB64.IsSet(idx) {
			t.Errorf("chunk %d should be set", idx)
		}
	}
	if fromB64.CompletedCount() != len(data.Completed) {
		t.Errorf("completed count mismatch: got %d, want %d", fromB64.CompletedCount(), len(data.Completed))
	}

	// 3. Test invalid examples
	for _, inv := range data.InvalidExamples {
		t.Run(inv.Note, func(t *testing.T) {
			_, err := FromBase64(inv.ChunkCount, inv.BitmapBase64)
			if err == nil {
				t.Errorf("expected error for invalid bitmap (%s), got nil", inv.Note)
			}
		})
	}
}

func TestManifestHashVector(t *testing.T) {
	root := findProjectRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testvectors", "manifest_hash.json"))
	if err != nil {
		t.Fatalf("read manifest_hash.json failed: %v", err)
	}

	var data struct {
		Entries          []ManifestEntry `json:"entries"`
		CanonicalUTF8Hex string          `json:"canonical_utf8_hex"`
		ManifestHash     string          `json:"manifest_hash"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal manifest_hash.json failed: %v", err)
	}

	var canonicalBuffer bytes.Buffer
	for _, e := range data.Entries {
		canonicalBuffer.WriteString(FormatCanonicalManifestLine(e))
	}

	if hex.EncodeToString(canonicalBuffer.Bytes()) != data.CanonicalUTF8Hex {
		t.Errorf("canonical utf8 hex mismatch:\ngot:  %s\nwant: %s", hex.EncodeToString(canonicalBuffer.Bytes()), data.CanonicalUTF8Hex)
	}

	computedHash := ComputeManifestHash(data.Entries)
	if computedHash != data.ManifestHash {
		t.Errorf("computed manifest hash mismatch:\ngot:  %s\nwant: %s", computedHash, data.ManifestHash)
	}
}
