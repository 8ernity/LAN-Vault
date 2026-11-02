package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreOperations(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "lanvault_test.db")

	s, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer s.Close()

	// 1. Trusted devices
	dev := TrustedDevice{
		Fingerprint: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		DeviceID:    "abcdef1234567890abcdef1234567890",
		Name:        "Test Laptop",
		Platform:    "windows",
		PairedAt:    time.Now().UnixMilli(),
		AutoAccept:  false,
		LastSeen:    time.Now().UnixMilli(),
	}
	if err := s.SaveTrustedDevice(dev); err != nil {
		t.Fatalf("SaveTrustedDevice failed: %v", err)
	}

	fetchedDev, err := s.GetTrustedDevice(dev.Fingerprint)
	if err != nil {
		t.Fatalf("GetTrustedDevice failed: %v", err)
	}
	if fetchedDev.Name != dev.Name {
		t.Errorf("device name mismatch: got %s, want %s", fetchedDev.Name, dev.Name)
	}

	devs, err := s.ListTrustedDevices()
	if err != nil || len(devs) != 1 {
		t.Fatalf("ListTrustedDevices returned %d devices, err: %v", len(devs), err)
	}

	// 2. Transfer and Files
	transfer := TransferRecord{
		TransferID:      "test-uuid-1",
		Direction:       "receive",
		PeerFingerprint: dev.Fingerprint,
		PeerName:        dev.Name,
		PeerDeviceID:    dev.DeviceID,
		State:           "Transferring",
		ChunkSize:       4194304,
		TotalFiles:      2,
		TotalBytes:      5000000,
		CompletedFiles:  0,
		CompletedBytes:  0,
		ManifestHash:    "dummyhash123",
		CreatedAt:       time.Now().UnixMilli(),
		UpdatedAt:       time.Now().UnixMilli(),
	}
	if err := s.SaveTransfer(transfer); err != nil {
		t.Fatalf("SaveTransfer failed: %v", err)
	}

	files := []TransferFileRecord{
		{
			TransferID:  transfer.TransferID,
			FileIndex:   0,
			Type:        "file",
			WirePath:    "doc.pdf",
			FinalPath:   "doc.pdf",
			Size:        4194304,
			MtimeMs:     1730000000000,
			ChunkCount:  1,
			ChunkBitmap: []byte{0x00},
			IsCompleted: false,
		},
		{
			TransferID:  transfer.TransferID,
			FileIndex:   1,
			Type:        "file",
			WirePath:    "pic.png",
			FinalPath:   "pic.png",
			Size:        805696,
			MtimeMs:     1730000001000,
			ChunkCount:  1,
			ChunkBitmap: []byte{0x00},
			IsCompleted: false,
		},
	}
	if err := s.SaveTransferFiles(files); err != nil {
		t.Fatalf("SaveTransferFiles failed: %v", err)
	}

	// Update bitmap for file 0
	if err := s.UpdateFileBitmap(transfer.TransferID, 0, []byte{0x01}, true); err != nil {
		t.Fatalf("UpdateFileBitmap failed: %v", err)
	}

	fetchedFiles, err := s.GetTransferFiles(transfer.TransferID)
	if err != nil || len(fetchedFiles) != 2 {
		t.Fatalf("GetTransferFiles returned %d files, err: %v", len(fetchedFiles), err)
	}
	if !fetchedFiles[0].IsCompleted || fetchedFiles[0].ChunkBitmap[0] != 0x01 {
		t.Errorf("file 0 completion state not updated: %+v", fetchedFiles[0])
	}

	// 3. Settings
	if err := s.SetSetting("general.theme", "dark"); err != nil {
		t.Fatalf("SetSetting failed: %v", err)
	}
	if val := s.GetSetting("general.theme", "system"); val != "dark" {
		t.Errorf("GetSetting got %s, want dark", val)
	}
	if val := s.GetSetting("nonexistent", "default"); val != "default" {
		t.Errorf("GetSetting default failed: got %s", val)
	}
}
