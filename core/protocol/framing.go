package protocol

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var (
	ErrFrameTooLarge    = errors.New("frame exceeds maximum allowed size")
	ErrInvalidFrameSize = errors.New("frame length is zero or invalid")
	ErrShortRead        = errors.New("unexpected end of stream while reading frame")
	ErrInvalidPayload   = errors.New("invalid payload structure for frame type")
)

// Frame represents a decoded protocol frame
type Frame struct {
	Type    FrameType
	Payload []byte
}

// ReadFrame reads a single frame from an io.Reader
func ReadFrame(r io.Reader) (*Frame, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(lenBuf[:])
	if length == 0 {
		return nil, ErrInvalidFrameSize
	}

	var typeBuf [1]byte
	if _, err := io.ReadFull(r, typeBuf[:]); err != nil {
		return nil, err
	}
	frameType := FrameType(typeBuf[0])

	// Check limits BEFORE allocating
	payloadLen := length - 1
	if frameType == FrameData {
		if length > MaxDataFrame {
			return nil, fmt.Errorf("%w: DATA frame length %d exceeds %d", ErrFrameTooLarge, length, MaxDataFrame)
		}
	} else {
		if length > MaxControlFrame {
			return nil, fmt.Errorf("%w: control frame length %d exceeds %d", ErrFrameTooLarge, length, MaxControlFrame)
		}
	}

	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
	}

	return &Frame{
		Type:    frameType,
		Payload: payload,
	}, nil
}

// WriteRawFrame writes length + type + payload to an io.Writer
func WriteRawFrame(w io.Writer, frameType FrameType, payload []byte) error {
	totalLen := uint32(1 + len(payload))
	if frameType == FrameData {
		if totalLen > MaxDataFrame {
			return fmt.Errorf("%w: DATA frame length %d exceeds %d", ErrFrameTooLarge, totalLen, MaxDataFrame)
		}
	} else {
		if totalLen > MaxControlFrame {
			return fmt.Errorf("%w: control frame length %d exceeds %d", ErrFrameTooLarge, totalLen, MaxControlFrame)
		}
	}

	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header[0:4], totalLen)
	header[4] = byte(frameType)

	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// WriteJSONFrame encodes a JSON object into a frame payload and writes it
func WriteJSONFrame(w io.Writer, frameType FrameType, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return WriteRawFrame(w, frameType, data)
}

// DataFramePayload holds decoded fields of a DATA frame
type DataFramePayload struct {
	FileIndex  uint32
	ChunkIndex uint32
	Data       []byte
	Hash       [32]byte
}

// EncodeDataFrame serializes a DATA frame with file_index, chunk_index, data, and SHA-256 hash
func EncodeDataFrame(fileIndex, chunkIndex uint32, data []byte) []byte {
	hash := sha256.Sum256(data)
	payload := make([]byte, 8+len(data)+32)
	binary.BigEndian.PutUint32(payload[0:4], fileIndex)
	binary.BigEndian.PutUint32(payload[4:8], chunkIndex)
	copy(payload[8:8+len(data)], data)
	copy(payload[8+len(data):], hash[:])
	return payload
}

// DecodeDataFramePayload decodes the binary payload of a DATA frame
func DecodeDataFramePayload(payload []byte) (*DataFramePayload, error) {
	// Must have at least 8 (indices) + 32 (hash) = 40 bytes
	if len(payload) < 40 {
		return nil, fmt.Errorf("%w: DATA payload length %d is less than header (40 bytes)", ErrInvalidPayload, len(payload))
	}

	fileIndex := binary.BigEndian.Uint32(payload[0:4])
	chunkIndex := binary.BigEndian.Uint32(payload[4:8])
	dataLen := len(payload) - 8 - 32
	data := payload[8 : 8+dataLen]

	var hash [32]byte
	copy(hash[:], payload[8+dataLen:])

	return &DataFramePayload{
		FileIndex:  fileIndex,
		ChunkIndex: chunkIndex,
		Data:       data,
		Hash:       hash,
	}, nil
}

// IndexPair represents (file_index, chunk_index)
type IndexPair struct {
	FileIndex  uint32
	ChunkIndex uint32
}

// EncodeIndexPairs encodes pairs into binary for CHUNK_ACK or CHUNK_NEED
func EncodeIndexPairs(pairs []IndexPair) []byte {
	buf := make([]byte, len(pairs)*8)
	for i, p := range pairs {
		binary.BigEndian.PutUint32(buf[i*8:i*8+4], p.FileIndex)
		binary.BigEndian.PutUint32(buf[i*8+4:i*8+8], p.ChunkIndex)
	}
	return buf
}

// DecodeIndexPairs decodes binary payload into IndexPair slice
func DecodeIndexPairs(payload []byte) ([]IndexPair, error) {
	if len(payload)%8 != 0 {
		return nil, fmt.Errorf("%w: index pairs payload length %d is not a multiple of 8", ErrInvalidPayload, len(payload))
	}
	count := len(payload) / 8
	pairs := make([]IndexPair, count)
	for i := 0; i < count; i++ {
		pairs[i] = IndexPair{
			FileIndex:  binary.BigEndian.Uint32(payload[i*8 : i*8+4]),
			ChunkIndex: binary.BigEndian.Uint32(payload[i*8+4 : i*8+8]),
		}
	}
	return pairs, nil
}
