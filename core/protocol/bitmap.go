package protocol

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

var (
	ErrInvalidBitmapLength = errors.New("bitmap byte length does not match expected ceil(chunk_count/8)")
	ErrBitBeyondChunkCount = errors.New("bitmap has bit set beyond chunk_count limit")
	ErrIndexOutOfBounds    = errors.New("chunk index is out of bounds")
)

// Bitmap manages a chunk completion bitset
type Bitmap struct {
	chunkCount int
	bytes      []byte
}

// NewBitmap creates an empty bitmap for given chunk count
func NewBitmap(chunkCount int) *Bitmap {
	byteLen := (chunkCount + 7) / 8
	return &Bitmap{
		chunkCount: chunkCount,
		bytes:      make([]byte, byteLen),
	}
}

// ChunkCount returns the total chunk count
func (b *Bitmap) ChunkCount() int {
	return b.chunkCount
}

// Bytes returns the raw byte slice
func (b *Bitmap) Bytes() []byte {
	return b.bytes
}

// Set marks chunkIndex as completed
func (b *Bitmap) Set(chunkIndex int) error {
	if chunkIndex < 0 || chunkIndex >= b.chunkCount {
		return ErrIndexOutOfBounds
	}
	byteIdx := chunkIndex / 8
	bitIdx := chunkIndex % 8
	b.bytes[byteIdx] |= (1 << bitIdx)
	return nil
}

// Clear marks chunkIndex as not completed
func (b *Bitmap) Clear(chunkIndex int) error {
	if chunkIndex < 0 || chunkIndex >= b.chunkCount {
		return ErrIndexOutOfBounds
	}
	byteIdx := chunkIndex / 8
	bitIdx := chunkIndex % 8
	b.bytes[byteIdx] &^= (1 << bitIdx)
	return nil
}

// IsSet returns true if chunkIndex is completed
func (b *Bitmap) IsSet(chunkIndex int) bool {
	if chunkIndex < 0 || chunkIndex >= b.chunkCount {
		return false
	}
	byteIdx := chunkIndex / 8
	bitIdx := chunkIndex % 8
	return (b.bytes[byteIdx] & (1 << bitIdx)) != 0
}

// Has returns true if chunkIndex is completed (alias for IsSet)
func (b *Bitmap) Has(chunkIndex int) bool {
	return b.IsSet(chunkIndex)
}

// CompletedCount returns the number of completed chunks
func (b *Bitmap) CompletedCount() int {
	count := 0
	for i := 0; i < b.chunkCount; i++ {
		if b.IsSet(i) {
			count++
		}
	}
	return count
}

// IsComplete returns true if all chunks are completed
func (b *Bitmap) IsComplete() bool {
	return b.CompletedCount() == b.chunkCount
}

// ToHex returns hex string representation
func (b *Bitmap) ToHex() string {
	return hex.EncodeToString(b.bytes)
}

// ToBase64 returns standard base64 string with padding
func (b *Bitmap) ToBase64() string {
	return base64.StdEncoding.EncodeToString(b.bytes)
}

// FromBytes validates and creates a Bitmap from raw bytes
func FromBytes(chunkCount int, data []byte) (*Bitmap, error) {
	expectedLen := (chunkCount + 7) / 8
	if len(data) != expectedLen {
		return nil, fmt.Errorf("%w: expected %d bytes for %d chunks, got %d", ErrInvalidBitmapLength, expectedLen, chunkCount, len(data))
	}

	// Verify no unused high bits are set in the last byte
	if chunkCount > 0 && chunkCount%8 != 0 {
		lastByteUnusedStart := chunkCount % 8
		mask := byte(0xFF << lastByteUnusedStart)
		if (data[expectedLen-1] & mask) != 0 {
			return nil, ErrBitBeyondChunkCount
		}
	}

	bmpBytes := make([]byte, len(data))
	copy(bmpBytes, data)
	return &Bitmap{
		chunkCount: chunkCount,
		bytes:      bmpBytes,
	}, nil
}

// FromHex decodes hex string into Bitmap
func FromHex(chunkCount int, hexStr string) (*Bitmap, error) {
	data, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, err
	}
	return FromBytes(chunkCount, data)
}

// FromBase64 decodes base64 string into Bitmap
func FromBase64(chunkCount int, b64Str string) (*Bitmap, error) {
	data, err := base64.StdEncoding.DecodeString(b64Str)
	if err != nil {
		return nil, err
	}
	return FromBytes(chunkCount, data)
}
