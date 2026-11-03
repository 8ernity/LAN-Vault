package transfer

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lanvault/core/identity"
	"lanvault/core/protocol"
	"lanvault/core/store"
)

// startTestReceiver starts the real receiver and returns its address plus a
// channel that reports the control handler's result.
func startTestReceiver(t *testing.T, receiverID *identity.Identity, st *store.Store, destDir string, requireDecision bool) (string, <-chan error, func()) {
	t.Helper()
	listener, err := tls.Listen("tcp", "127.0.0.1:0", receiverID.ServerTLSConfig())
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	onDecision := func(offer protocol.OfferMessage, peerName, peerFP string) (bool, string) {
		if requireDecision {
			t.Errorf("resume must not re-prompt the user")
		}
		return true, destDir
	}
	errChan := make(chan error, 2)
	go func() {
		for {
			ctrlConn, err := listener.Accept()
			if err != nil {
				errChan <- err
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				errChan <- HandleIncomingControl(c, receiverID, st, onDecision, listener)
			}(ctrlConn)
		}
	}()
	return listener.Addr().String(), errChan, func() { _ = listener.Close() }
}

// readAck waits for the CHUNK_ACK that corresponds to fileIndex/chunkIndex.
func readAck(t *testing.T, conn *tls.Conn, fileIndex, chunkIndex int) {
	t.Helper()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		frame, err := protocol.ReadFrame(conn)
		if err != nil {
			t.Fatalf("read ACK failed: %v", err)
		}
		if frame.Type != protocol.FrameChunkAck {
			continue
		}
		pairs, err := protocol.DecodeIndexPairs(frame.Payload)
		if err != nil {
			t.Fatalf("decode ACK failed: %v", err)
		}
		for _, p := range pairs {
			if int(p.FileIndex) == fileIndex && int(p.ChunkIndex) == chunkIndex {
				return
			}
		}
	}
}

func TestResumeAfterInterruption(t *testing.T) {
	sourceDir := t.TempDir()
	file1 := filepath.Join(sourceDir, "first.bin")
	file2 := filepath.Join(sourceDir, "second.bin")
	if err := os.WriteFile(file1, bytes.Repeat([]byte("A"), 200000), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, bytes.Repeat([]byte("B"), 150000), 0644); err != nil {
		t.Fatal(err)
	}

	items, totalFiles, totalBytes, manifestHash, err := ScanSourceItems([]string{sourceDir})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	destDir := t.TempDir()

	receiverID, err := identity.GenerateNewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	senderID, err := identity.GenerateNewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SaveTrustedDevice(store.TrustedDevice{
		Fingerprint: senderID.Fingerprint, DeviceID: senderID.DeviceID,
		Name: "ResumeSender", Platform: "windows",
		PairedAt: time.Now().UnixMilli(), LastSeen: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}

	addr, errChan, closeListener := startTestReceiver(t, receiverID, st, destDir, false)
	defer closeListener()

	transferID := "resume-test-transfer"
	chunkSize := int64(65536)

	// First session: send the first file completely and only one chunk of the
	// second file, then drop both connections without DONE.
	ctrl, err := tls.Dial("tcp", addr, senderID.ClientTLSConfigPinned(receiverID.Fingerprint))
	if err != nil {
		t.Fatalf("control dial failed: %v", err)
	}
	if err := protocol.WriteJSONFrame(ctrl, protocol.FrameHello, protocol.HelloMessage{
		Version: protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
		DeviceID: senderID.DeviceID, Name: "ResumeSender", Platform: "windows", Role: "control",
	}); err != nil {
		t.Fatal(err)
	}
	if f, err := protocol.ReadFrame(ctrl); err != nil || f.Type != protocol.FrameHelloOk {
		t.Fatalf("HELLO_OK failed: %v", err)
	}
	offer := protocol.OfferMessage{
		TransferID: transferID, ChunkSize: chunkSize, FileCount: totalFiles,
		DirCount: 1, TotalBytes: totalBytes, DataStreams: 1,
	}
	// The scan includes the root directory entry.
	for _, it := range items {
		if it.Entry.Type == "dir" {
			offer.DirCount = 1
		} else {
			offer.DirCount = 0
		}
	}
	// Recompute counts directly from the scanned manifest.
	files, dirs := 0, 0
	for _, it := range items {
		if it.Entry.Type == "file" {
			files++
		} else {
			dirs++
		}
	}
	offer.FileCount = files
	offer.DirCount = dirs
	offer.TotalBytes = totalBytes
	if err := protocol.WriteJSONFrame(ctrl, protocol.FrameOffer, offer); err != nil {
		t.Fatal(err)
	}
	var manifest []protocol.ManifestEntry
	for _, it := range items {
		manifest = append(manifest, it.Entry)
	}
	if err := protocol.WriteJSONFrame(ctrl, protocol.FrameManifestPart, protocol.ManifestPartMessage{Entries: manifest}); err != nil {
		t.Fatal(err)
	}
	if err := protocol.WriteJSONFrame(ctrl, protocol.FrameManifestEnd, protocol.ManifestEndMessage{
		EntryCount: len(manifest), ManifestHash: manifestHash,
	}); err != nil {
		t.Fatal(err)
	}
	f, err := protocol.ReadFrame(ctrl)
	if err != nil || f.Type != protocol.FrameAccept {
		t.Fatalf("ACCEPT failed: type=%v err=%v", f, err)
	}
	accept, err := protocol.DecodeJSONPayload[protocol.AcceptMessage](f.Payload)
	if err != nil {
		t.Fatal(err)
	}

	dataConn, err := tls.Dial("tcp", addr, senderID.ClientTLSConfigPinned(receiverID.Fingerprint))
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.WriteJSONFrame(dataConn, protocol.FrameHello, protocol.HelloMessage{
		Version: protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
		DeviceID: senderID.DeviceID, Name: "ResumeSender", Platform: "windows",
		Role: "data", TransferID: transferID, StreamToken: accept.StreamToken,
	}); err != nil {
		t.Fatal(err)
	}
	if df, err := protocol.ReadFrame(dataConn); err != nil || df.Type != protocol.FrameHelloOk {
		t.Fatalf("data HELLO_OK failed: %v", err)
	}

	sentFirstFile := 0
	for _, it := range items {
		if it.Entry.Type != "file" || it.Entry.Index != 0 {
			continue
		}
		numChunks := int((it.Entry.Size + chunkSize - 1) / chunkSize)
		fh, err := os.Open(it.SourceDiskPath)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, chunkSize)
		for c := 0; c < numChunks; c++ {
			n, err := fh.Read(buf)
			if err != nil && n == 0 {
				fh.Close()
				t.Fatalf("read source failed: %v", err)
			}
			if err := protocol.WriteRawFrame(dataConn, protocol.FrameData,
				protocol.EncodeDataFrame(uint32(it.Entry.Index), uint32(c), buf[:n])); err != nil {
				fh.Close()
				t.Fatal(err)
			}
			readAck(t, ctrl, it.Entry.Index, c)
			sentFirstFile++
		}
		fh.Close()
	}
	// Send exactly one chunk of the second file.
	for _, it := range items {
		if it.Entry.Type != "file" || it.Entry.Index != 1 {
			continue
		}
		fh, err := os.Open(it.SourceDiskPath)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, chunkSize)
		n, _ := fh.Read(buf)
		fh.Close()
		if err := protocol.WriteRawFrame(dataConn, protocol.FrameData,
			protocol.EncodeDataFrame(uint32(it.Entry.Index), 0, buf[:n])); err != nil {
			t.Fatal(err)
		}
		readAck(t, ctrl, it.Entry.Index, 0)
	}

	// Abrupt disconnect: no DONE, no CANCEL.
	dataConn.Close()
	ctrl.Close()
	if err := <-errChan; err == nil {
		t.Fatal("expected interrupted receiver session to return an error")
	}

	// Resume with the same transfer ID. The receiver must reuse persisted
	// bitmap/hash evidence and must not call onDecision.
	doneOk, err := StartSend(
		addr, senderID, "ResumeSender", transferID, items, totalFiles, totalBytes,
		manifestHash, chunkSize, nil, receiverID.Fingerprint,
	)
	if err != nil {
		t.Fatalf("resumed StartSend failed: %v", err)
	}
	if doneOk.FilesCompleted != files {
		t.Fatalf("files completed = %d, want %d", doneOk.FilesCompleted, files)
	}

	rec1, err := os.ReadFile(filepath.Join(destDir, filepath.Base(sourceDir), "first.bin"))
	if err != nil {
		t.Fatalf("read resumed file1: %v", err)
	}
	orig1, _ := os.ReadFile(file1)
	if !bytes.Equal(rec1, orig1) {
		t.Fatal("resumed file1 content mismatch")
	}
	rec2, err := os.ReadFile(filepath.Join(destDir, filepath.Base(sourceDir), "second.bin"))
	if err != nil {
		t.Fatalf("read resumed file2: %v", err)
	}
	orig2, _ := os.ReadFile(file2)
	if !bytes.Equal(rec2, orig2) {
		t.Fatal("resumed file2 content mismatch")
	}
	_ = sentFirstFile
}

func TestUnpairedSenderRejectedByDefault(t *testing.T) {
	destDir := t.TempDir()
	receiverID, err := identity.GenerateNewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	senderID, err := identity.GenerateNewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	addr, errChan, closeListener := startTestReceiver(t, receiverID, st, destDir, false)
	defer closeListener()

	source := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(source, []byte("unpaired"), 0644); err != nil {
		t.Fatal(err)
	}
	items, totalFiles, totalBytes, manifestHash, err := ScanSourceItems([]string{source})
	if err != nil {
		t.Fatal(err)
	}
	_, err = StartSend(addr, senderID, "Unpaired", "unpaired-test", items, totalFiles, totalBytes,
		manifestHash, 65536, nil, receiverID.Fingerprint)
	if err == nil {
		t.Fatal("expected unpaired send to fail")
	}
	if got := <-errChan; got == nil {
		t.Fatal("expected receiver to reject the unpaired offer")
	}
	if _, err := os.Stat(filepath.Join(destDir, "x.txt")); !os.IsNotExist(err) {
		t.Fatal("unpaired transfer must not create a destination file")
	}
	_ = fmt.Sprintf("%v", err)
}
