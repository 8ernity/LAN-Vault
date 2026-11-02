package transfer

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"lanvault/core/identity"
	"lanvault/core/protocol"
	"lanvault/core/store"
)

func TestEndToEndTransfer(t *testing.T) {
	// 1. Create source test directory structure
	sourceDir := t.TempDir()
	file1Path := filepath.Join(sourceDir, "hello.txt")
	os.WriteFile(file1Path, []byte("Hello, LANVault offline transfer!"), 0644)

	subDir := filepath.Join(sourceDir, "docs")
	os.MkdirAll(subDir, 0755)
	file2Path := filepath.Join(subDir, "large.bin")
	largeData := bytes.Repeat([]byte("LANVaultChunkTestData1234567890"), 10000) // ~300 KB
	os.WriteFile(file2Path, largeData, 0644)

	destDir := t.TempDir()

	// 2. Generate identities
	receiverID, err := identity.GenerateNewIdentity()
	if err != nil {
		t.Fatalf("receiverID failed: %v", err)
	}
	senderID, err := identity.GenerateNewIdentity()
	if err != nil {
		t.Fatalf("senderID failed: %v", err)
	}

	// 3. Open receiver database store
	st, err := store.OpenStore(":memory:")
	if err != nil {
		t.Fatalf("store failed: %v", err)
	}
	defer st.Close()

	// 4. Start receiver listener
	serverTLS := receiverID.ServerTLSConfig()
	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer listener.Close()

	receiverAddr := listener.Addr().String()

	// Decision callback that accepts transfer into destDir
	onDecision := func(offer protocol.OfferMessage, peerName, peerFP string) (bool, string) {
		return true, destDir
	}

	errChan := make(chan error, 1)
	go func() {
		// Accept control connection
		ctrlConn, err := listener.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer ctrlConn.Close()

		if err := HandleIncomingControl(ctrlConn, receiverID, st, onDecision, listener); err != nil {
			errChan <- err
			return
		}
		errChan <- nil
	}()

	// 5. Scan source items
	items, totalFiles, totalBytes, manifestHash, err := ScanSourceItems([]string{sourceDir})
	if err != nil {
		t.Fatalf("ScanSourceItems failed: %v", err)
	}

	// 6. Execute sender
	transferID := "test-transfer-e2e-001"
	chunkSize := int64(65536) // 64 KiB chunks for testing multi-chunking

	doneOk, err := StartSend(
		receiverAddr,
		senderID,
		"TestSender",
		transferID,
		items,
		totalFiles,
		totalBytes,
		manifestHash,
		chunkSize,
		nil,
	)
	if err != nil {
		t.Fatalf("StartSend failed: %v", err)
	}

	if doneOk.FilesCompleted != 2 {
		t.Errorf("files completed mismatch: got %d, want 2", doneOk.FilesCompleted)
	}

	if err := <-errChan; err != nil {
		t.Fatalf("receiver error: %v", err)
	}

	// 7. Verify received files match source files exactly
	// Check hello.txt
	receivedFile1 := filepath.Join(destDir, filepath.Base(sourceDir), "hello.txt")
	rec1Data, err := os.ReadFile(receivedFile1)
	if err != nil {
		t.Fatalf("read received file1 failed: %v", err)
	}
	if string(rec1Data) != "Hello, LANVault offline transfer!" {
		t.Errorf("file1 content mismatch: %s", string(rec1Data))
	}

	// Check large.bin SHA-256
	receivedFile2 := filepath.Join(destDir, filepath.Base(sourceDir), "docs", "large.bin")
	rec2Data, err := os.ReadFile(receivedFile2)
	if err != nil {
		t.Fatalf("read received file2 failed: %v", err)
	}
	origHash := sha256.Sum256(largeData)
	recHash := sha256.Sum256(rec2Data)
	if origHash != recHash {
		t.Errorf("file2 SHA-256 mismatch: got %s, want %s", hex.EncodeToString(recHash[:]), hex.EncodeToString(origHash[:]))
	}

	t.Logf("End-to-end file transfer verified: %d files, %d bytes identical!", doneOk.FilesCompleted, doneOk.Bytes)
}
