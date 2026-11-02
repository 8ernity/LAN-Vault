package protocol

import "encoding/json"

// Version specifies wire protocol version
type Version struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

// HelloMessage (0x01)
type HelloMessage struct {
	Version      Version  `json:"version"`
	DeviceID     string   `json:"device_id"`
	Name         string   `json:"name"`
	Platform     string   `json:"platform"`
	Role         string   `json:"role"` // "control" or "data"
	Capabilities []string `json:"capabilities,omitempty"`
	TransferID   string   `json:"transfer_id,omitempty"`
	StreamToken  string   `json:"stream_token,omitempty"`
}

// HelloOkMessage (0x02)
type HelloOkMessage struct {
	Version      Version  `json:"version"`
	DeviceID     string   `json:"device_id"`
	Name         string   `json:"name"`
	Platform     string   `json:"platform"`
	Capabilities []string `json:"capabilities,omitempty"`
	Paired       bool     `json:"paired"`
}

// OfferMessage (0x10)
type OfferMessage struct {
	TransferID  string `json:"transfer_id"`
	ChunkSize   int64  `json:"chunk_size"`
	FileCount   int    `json:"file_count"`
	DirCount    int    `json:"dir_count"`
	TotalBytes  int64  `json:"total_bytes"`
	DataStreams int    `json:"data_streams"`
}

// ManifestEntry represents a file or directory in the manifest
type ManifestEntry struct {
	Index   int    `json:"index"`
	Type    string `json:"type"` // "file" or "dir"
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	MtimeMs int64  `json:"mtime_ms"`
}

// ManifestPartMessage (0x11)
type ManifestPartMessage struct {
	Entries []ManifestEntry `json:"entries"`
}

// ManifestEndMessage (0x12)
type ManifestEndMessage struct {
	EntryCount   int    `json:"entry_count"`
	ManifestHash string `json:"manifest_hash"`
}

// AcceptMessage (0x13)
type AcceptMessage struct {
	TransferID  string            `json:"transfer_id"`
	Resume      bool              `json:"resume"`
	StreamToken string            `json:"stream_token"`
	DataStreams int               `json:"data_streams"`
	Have        map[string]string `json:"have,omitempty"` // file_index string -> base64 bitmap
}

// RejectMessage (0x14)
type RejectMessage struct {
	TransferID string `json:"transfer_id"`
	Code       int    `json:"code"`
	Message    string `json:"message"`
}

// FileDoneMessage (0x22)
type FileDoneMessage struct {
	Index int `json:"index"`
}

// DoneMessage (0x40)
type DoneMessage struct {
	TransferID string `json:"transfer_id"`
}

// DoneOkMessage (0x41)
type DoneOkMessage struct {
	TransferID     string `json:"transfer_id"`
	FilesCompleted int    `json:"files_completed"`
	Bytes          int64  `json:"bytes"`
}

// PauseMessage (0x30)
type PauseMessage struct {
	TransferID string `json:"transfer_id"`
	Reason     string `json:"reason,omitempty"`
}

// ResumeMessage (0x31)
type ResumeMessage struct {
	TransferID string `json:"transfer_id"`
	Reason     string `json:"reason,omitempty"`
}

// CancelMessage (0x32)
type CancelMessage struct {
	TransferID string `json:"transfer_id"`
	Reason     string `json:"reason,omitempty"`
}

// PairRequestMessage (0x50)
type PairRequestMessage struct {
	Token    string `json:"token"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

// PairOkMessage (0x51)
type PairOkMessage struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
}

// ErrorMessage (0x7F)
type ErrorMessage struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	FileIndex  *int   `json:"file_index,omitempty"`
	ChunkIndex *int   `json:"chunk_index,omitempty"`
	Fatal      bool   `json:"fatal"`
}

// Helper to decode JSON payload safely into target struct
func DecodeJSONPayload[T any](payload []byte) (*T, error) {
	var target T
	if err := json.Unmarshal(payload, &target); err != nil {
		return nil, err
	}
	return &target, nil
}
