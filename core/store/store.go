package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("record not found")
)

// TrustedDevice models a paired device in the trust store
type TrustedDevice struct {
	Fingerprint string `json:"fingerprint"`
	DeviceID    string `json:"device_id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	PairedAt    int64  `json:"paired_at"`
	AutoAccept  bool   `json:"auto_accept"`
	LastSeen    int64  `json:"last_seen"`
}

// TransferRecord models an ongoing or past transfer
type TransferRecord struct {
	TransferID      string `json:"transfer_id"`
	Direction       string `json:"direction"` // "send" or "receive"
	PeerFingerprint string `json:"peer_fingerprint"`
	PeerName        string `json:"peer_name"`
	PeerDeviceID    string `json:"peer_device_id"`
	State           string `json:"state"`
	ChunkSize       int64  `json:"chunk_size"`
	TotalFiles      int    `json:"total_files"`
	TotalBytes      int64  `json:"total_bytes"`
	CompletedFiles  int    `json:"completed_files"`
	CompletedBytes  int64  `json:"completed_bytes"`
	ManifestHash    string `json:"manifest_hash"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
	ErrorMessage    string `json:"error_message,omitempty"`
}

// TransferFileRecord models an entry in a transfer manifest
type TransferFileRecord struct {
	TransferID  string `json:"transfer_id"`
	FileIndex   int    `json:"file_index"`
	Type        string `json:"type"` // "file" or "dir"
	WirePath    string `json:"wire_path"`
	FinalPath   string `json:"final_path"`
	Size        int64  `json:"size"`
	MtimeMs     int64  `json:"mtime_ms"`
	ChunkCount  int    `json:"chunk_count"`
	ChunkBitmap []byte `json:"chunk_bitmap,omitempty"`
	IsCompleted bool   `json:"is_completed"`
}

// Store manages SQLite database operations
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// OpenStore opens or creates a SQLite database at dbPath and applies migrations
func OpenStore(dbPath string) (*Store, error) {
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// WAL mode and busy timeout for reliability
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA synchronous=NORMAL;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %s failed: %w", pragma, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}
	return s, nil
}

// Close closes the database connection
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS trusted_devices (
		fingerprint TEXT PRIMARY KEY,
		device_id TEXT NOT NULL,
		name TEXT NOT NULL,
		platform TEXT NOT NULL,
		paired_at INTEGER NOT NULL,
		auto_accept INTEGER NOT NULL DEFAULT 0,
		last_seen INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS transfers (
		transfer_id TEXT PRIMARY KEY,
		direction TEXT NOT NULL,
		peer_fingerprint TEXT NOT NULL,
		peer_name TEXT NOT NULL,
		peer_device_id TEXT NOT NULL,
		state TEXT NOT NULL,
		chunk_size INTEGER NOT NULL,
		total_files INTEGER NOT NULL,
		total_bytes INTEGER NOT NULL,
		completed_files INTEGER NOT NULL DEFAULT 0,
		completed_bytes INTEGER NOT NULL DEFAULT 0,
		manifest_hash TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		error_message TEXT
	);

	CREATE TABLE IF NOT EXISTS transfer_files (
		transfer_id TEXT NOT NULL,
		file_index INTEGER NOT NULL,
		type TEXT NOT NULL,
		wire_path TEXT NOT NULL,
		final_path TEXT NOT NULL,
		size INTEGER NOT NULL,
		mtime_ms INTEGER NOT NULL,
		chunk_count INTEGER NOT NULL,
		chunk_bitmap BLOB,
		is_completed INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (transfer_id, file_index),
		FOREIGN KEY (transfer_id) REFERENCES transfers(transfer_id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_transfers_created_at ON transfers(created_at DESC);
	`
	_, err := s.db.Exec(schema)
	return err
}

// SaveTrustedDevice adds or updates a trusted device
func (s *Store) SaveTrustedDevice(dev TrustedDevice) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO trusted_devices (fingerprint, device_id, name, platform, paired_at, auto_accept, last_seen)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(fingerprint) DO UPDATE SET
		name = excluded.name,
		platform = excluded.platform,
		auto_accept = excluded.auto_accept,
		last_seen = excluded.last_seen;
	`
	autoAccept := 0
	if dev.AutoAccept {
		autoAccept = 1
	}
	_, err := s.db.Exec(query, dev.Fingerprint, dev.DeviceID, dev.Name, dev.Platform, dev.PairedAt, autoAccept, dev.LastSeen)
	return err
}

// GetTrustedDevice fetches a trusted device by fingerprint
func (s *Store) GetTrustedDevice(fp string) (*TrustedDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT fingerprint, device_id, name, platform, paired_at, auto_accept, last_seen FROM trusted_devices WHERE fingerprint = ?`
	row := s.db.QueryRow(query, fp)

	var dev TrustedDevice
	var autoAccept int
	err := row.Scan(&dev.Fingerprint, &dev.DeviceID, &dev.Name, &dev.Platform, &dev.PairedAt, &autoAccept, &dev.LastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	dev.AutoAccept = (autoAccept == 1)
	return &dev, nil
}

// ListTrustedDevices returns all paired devices
func (s *Store) ListTrustedDevices() ([]TrustedDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT fingerprint, device_id, name, platform, paired_at, auto_accept, last_seen FROM trusted_devices ORDER BY last_seen DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TrustedDevice
	for rows.Next() {
		var dev TrustedDevice
		var autoAccept int
		if err := rows.Scan(&dev.Fingerprint, &dev.DeviceID, &dev.Name, &dev.Platform, &dev.PairedAt, &autoAccept, &dev.LastSeen); err != nil {
			return nil, err
		}
		dev.AutoAccept = (autoAccept == 1)
		result = append(result, dev)
	}
	return result, rows.Err()
}

// DeleteTrustedDevice removes a trusted device
func (s *Store) DeleteTrustedDevice(fp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM trusted_devices WHERE fingerprint = ?", fp)
	return err
}

// SaveTransfer creates or updates a transfer record
func (s *Store) SaveTransfer(t TransferRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO transfers (
		transfer_id, direction, peer_fingerprint, peer_name, peer_device_id, state,
		chunk_size, total_files, total_bytes, completed_files, completed_bytes,
		manifest_hash, created_at, updated_at, error_message
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(transfer_id) DO UPDATE SET
		state = excluded.state,
		completed_files = excluded.completed_files,
		completed_bytes = excluded.completed_bytes,
		updated_at = excluded.updated_at,
		error_message = excluded.error_message;
	`
	_, err := s.db.Exec(query,
		t.TransferID, t.Direction, t.PeerFingerprint, t.PeerName, t.PeerDeviceID, t.State,
		t.ChunkSize, t.TotalFiles, t.TotalBytes, t.CompletedFiles, t.CompletedBytes,
		t.ManifestHash, t.CreatedAt, t.UpdatedAt, t.ErrorMessage,
	)
	return err
}

// GetTransfer fetches transfer by ID
func (s *Store) GetTransfer(transferID string) (*TransferRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
	SELECT transfer_id, direction, peer_fingerprint, peer_name, peer_device_id, state,
	       chunk_size, total_files, total_bytes, completed_files, completed_bytes,
	       manifest_hash, created_at, updated_at, coalesce(error_message, '')
	FROM transfers WHERE transfer_id = ?
	`
	row := s.db.QueryRow(query, transferID)
	var t TransferRecord
	err := row.Scan(
		&t.TransferID, &t.Direction, &t.PeerFingerprint, &t.PeerName, &t.PeerDeviceID, &t.State,
		&t.ChunkSize, &t.TotalFiles, &t.TotalBytes, &t.CompletedFiles, &t.CompletedBytes,
		&t.ManifestHash, &t.CreatedAt, &t.UpdatedAt, &t.ErrorMessage,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

// ListRecentTransfers returns last N transfers
func (s *Store) ListRecentTransfers(limit int) ([]TransferRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
	SELECT transfer_id, direction, peer_fingerprint, peer_name, peer_device_id, state,
	       chunk_size, total_files, total_bytes, completed_files, completed_bytes,
	       manifest_hash, created_at, updated_at, coalesce(error_message, '')
	FROM transfers ORDER BY created_at DESC LIMIT ?
	`
	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TransferRecord
	for rows.Next() {
		var t TransferRecord
		if err := rows.Scan(
			&t.TransferID, &t.Direction, &t.PeerFingerprint, &t.PeerName, &t.PeerDeviceID, &t.State,
			&t.ChunkSize, &t.TotalFiles, &t.TotalBytes, &t.CompletedFiles, &t.CompletedBytes,
			&t.ManifestHash, &t.CreatedAt, &t.UpdatedAt, &t.ErrorMessage,
		); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

// SaveTransferFiles inserts or updates files in a transfer
func (s *Store) SaveTransferFiles(files []TransferFileRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
	INSERT INTO transfer_files (
		transfer_id, file_index, type, wire_path, final_path, size, mtime_ms, chunk_count, chunk_bitmap, is_completed
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(transfer_id, file_index) DO UPDATE SET
		chunk_bitmap = excluded.chunk_bitmap,
		is_completed = excluded.is_completed;
	`
	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, f := range files {
		isComp := 0
		if f.IsCompleted {
			isComp = 1
		}
		if _, err := stmt.Exec(
			f.TransferID, f.FileIndex, f.Type, f.WirePath, f.FinalPath, f.Size, f.MtimeMs, f.ChunkCount, f.ChunkBitmap, isComp,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpdateFileBitmap updates persisted bitmap and completion for one file
func (s *Store) UpdateFileBitmap(transferID string, fileIndex int, bitmap []byte, isCompleted bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	isComp := 0
	if isCompleted {
		isComp = 1
	}
	query := `UPDATE transfer_files SET chunk_bitmap = ?, is_completed = ? WHERE transfer_id = ? AND file_index = ?`
	_, err := s.db.Exec(query, bitmap, isComp, transferID, fileIndex)
	return err
}

// GetTransferFiles returns all file records for a transfer
func (s *Store) GetTransferFiles(transferID string) ([]TransferFileRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
	SELECT transfer_id, file_index, type, wire_path, final_path, size, mtime_ms, chunk_count, chunk_bitmap, is_completed
	FROM transfer_files WHERE transfer_id = ? ORDER BY file_index ASC
	`
	rows, err := s.db.Query(query, transferID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TransferFileRecord
	for rows.Next() {
		var f TransferFileRecord
		var isComp int
		if err := rows.Scan(
			&f.TransferID, &f.FileIndex, &f.Type, &f.WirePath, &f.FinalPath, &f.Size, &f.MtimeMs, &f.ChunkCount, &f.ChunkBitmap, &isComp,
		); err != nil {
			return nil, err
		}
		f.IsCompleted = (isComp == 1)
		result = append(result, f)
	}
	return result, rows.Err()
}

// SetSetting saves a key-value setting
func (s *Store) SetSetting(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`
	_, err := s.db.Exec(query, key, value)
	return err
}

// GetSetting reads a setting
func (s *Store) GetSetting(key string, defaultValue string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var val string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err != nil {
		return defaultValue
	}
	return val
}

// GetAllSettings returns map of all settings
func (s *Store) GetAllSettings() (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, rows.Err()
}

// CleanupOldTransfers deletes partial transfers older than retention days
func (s *Store) CleanupOldTransfers(retentionDays int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).UnixMilli()
	query := `DELETE FROM transfers WHERE created_at < ? AND state NOT IN ('Completed', 'CompletedWithErrors')`
	res, err := s.db.Exec(query, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
