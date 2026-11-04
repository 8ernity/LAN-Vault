package transfer

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lanvault/core/identity"
	"lanvault/core/pairing"
	"lanvault/core/protocol"
	"lanvault/core/store"
)

func TestAdversarialEarlyDoneRejected(t *testing.T) {
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
		Fingerprint: senderID.Fingerprint,
		DeviceID:    senderID.DeviceID,
		Name:        "AdversarialSender",
		Platform:    "windows",
		PairedAt:    time.Now().UnixMilli(),
		LastSeen:    time.Now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	addr, errChan, closeListener := startTestReceiver(t, receiverID, st, destDir, false)
	defer closeListener()

	ctrl, err := tls.Dial("tcp", addr, senderID.ClientTLSConfigPinned(receiverID.Fingerprint))
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	if err := protocol.WriteJSONFrame(ctrl, protocol.FrameHello, protocol.HelloMessage{
		Version:  protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
		DeviceID: senderID.DeviceID, Name: "Adversarial", Platform: "windows", Role: "control",
	}); err != nil {
		t.Fatal(err)
	}
	if f, err := protocol.ReadFrame(ctrl); err != nil || f.Type != protocol.FrameHelloOk {
		t.Fatalf("HELLO_OK failed: %v", err)
	}

	transferID := "early-done-test"
	manifest := []protocol.ManifestEntry{
		{Index: 0, Path: "incomplete.bin", Type: "file", Size: 100000, MtimeMs: time.Now().UnixMilli()},
	}
	manifestHash := protocol.ComputeManifestHash(manifest)

	if err := protocol.WriteJSONFrame(ctrl, protocol.FrameOffer, protocol.OfferMessage{
		TransferID: transferID, ChunkSize: 65536, FileCount: 1, DirCount: 0, TotalBytes: 100000, DataStreams: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := protocol.WriteJSONFrame(ctrl, protocol.FrameManifestPart, protocol.ManifestPartMessage{Entries: manifest}); err != nil {
		t.Fatal(err)
	}
	if err := protocol.WriteJSONFrame(ctrl, protocol.FrameManifestEnd, protocol.ManifestEndMessage{
		EntryCount: 1, ManifestHash: manifestHash,
	}); err != nil {
		t.Fatal(err)
	}
	f, err := protocol.ReadFrame(ctrl)
	if err != nil || f.Type != protocol.FrameAccept {
		t.Fatalf("expected ACCEPT, got %v", f)
	}

	// Send premature DONE without sending any data chunks
	if err := protocol.WriteJSONFrame(ctrl, protocol.FrameDone, protocol.DoneMessage{TransferID: transferID}); err != nil {
		t.Fatal(err)
	}

	// Receiver must send ERROR, not DONE_OK
	f, err = protocol.ReadFrame(ctrl)
	if err != nil {
		t.Fatalf("expected error frame from receiver: %v", err)
	}
	if f.Type != protocol.FrameError {
		t.Fatalf("expected FRAME_ERROR on early DONE, got %v", f.Type)
	}

	// Destination file must not exist
	if _, err := os.Stat(filepath.Join(destDir, "incomplete.bin")); !os.IsNotExist(err) {
		t.Fatal("destination file must not be finalized on premature DONE")
	}

	_ = <-errChan
}

func TestAdversarialUnauthorizedDataStream(t *testing.T) {
	receiverID, err := identity.GenerateNewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	legitSenderID, err := identity.GenerateNewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	attackerID, err := identity.GenerateNewIdentity()
	if err != nil {
		t.Fatal(err)
	}

	st, err := store.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.SaveTrustedDevice(store.TrustedDevice{
		Fingerprint: legitSenderID.Fingerprint,
		DeviceID:    legitSenderID.DeviceID,
		Name:        "LegitSender",
		Platform:    "windows",
		PairedAt:    time.Now().UnixMilli(),
		LastSeen:    time.Now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	addr, _, closeListener := startTestReceiver(t, receiverID, st, destDir, false)
	defer closeListener()

	// 1. Legit sender establishes control session and gets stream token
	ctrl, err := tls.Dial("tcp", addr, legitSenderID.ClientTLSConfigPinned(receiverID.Fingerprint))
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	_ = protocol.WriteJSONFrame(ctrl, protocol.FrameHello, protocol.HelloMessage{
		Version:  protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
		DeviceID: legitSenderID.DeviceID, Name: "Legit", Platform: "windows", Role: "control",
	})
	_, _ = protocol.ReadFrame(ctrl)

	transferID := "unauthorized-data-test"
	manifest := []protocol.ManifestEntry{
		{Index: 0, Path: "file.bin", Type: "file", Size: 100, MtimeMs: time.Now().UnixMilli()},
	}
	manifestHash := protocol.ComputeManifestHash(manifest)
	_ = protocol.WriteJSONFrame(ctrl, protocol.FrameOffer, protocol.OfferMessage{
		TransferID: transferID, ChunkSize: 65536, FileCount: 1, DirCount: 0, TotalBytes: 100, DataStreams: 1,
	})
	_ = protocol.WriteJSONFrame(ctrl, protocol.FrameManifestPart, protocol.ManifestPartMessage{Entries: manifest})
	_ = protocol.WriteJSONFrame(ctrl, protocol.FrameManifestEnd, protocol.ManifestEndMessage{
		EntryCount: 1, ManifestHash: manifestHash,
	})
	f, err := protocol.ReadFrame(ctrl)
	if err != nil || f.Type != protocol.FrameAccept {
		t.Fatalf("expected ACCEPT: %v", err)
	}
	accept, _ := protocol.DecodeJSONPayload[protocol.AcceptMessage](f.Payload)

	// 2. Attacker attempts to connect with legit stream token but attacker cert
	attackerConn, err := tls.Dial("tcp", addr, attackerID.ClientTLSConfigPinned(receiverID.Fingerprint))
	if err != nil {
		t.Fatal(err)
	}
	defer attackerConn.Close()

	_ = protocol.WriteJSONFrame(attackerConn, protocol.FrameHello, protocol.HelloMessage{
		Version:     protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
		DeviceID:    attackerID.DeviceID,
		Name:        "Attacker",
		Platform:    "windows",
		Role:        "data",
		TransferID:  transferID,
		StreamToken: accept.StreamToken,
	})
	af, err := protocol.ReadFrame(attackerConn)
	if err != nil {
		// Connection closed by receiver - good
	} else if af.Type != protocol.FrameError {
		t.Fatalf("expected receiver to reject attacker data connection, got %v", af.Type)
	}

	// 3. Third party with right cert but bogus stream token
	bogusDataConn, err := tls.Dial("tcp", addr, legitSenderID.ClientTLSConfigPinned(receiverID.Fingerprint))
	if err != nil {
		t.Fatal(err)
	}
	defer bogusDataConn.Close()

	_ = protocol.WriteJSONFrame(bogusDataConn, protocol.FrameHello, protocol.HelloMessage{
		Version:     protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
		DeviceID:    legitSenderID.DeviceID,
		Name:        "Legit",
		Platform:    "windows",
		Role:        "data",
		TransferID:  transferID,
		StreamToken: "invalid-stream-token-xyz",
	})
	bf, err := protocol.ReadFrame(bogusDataConn)
	if err != nil {
		// Closed - good
	} else if bf.Type != protocol.FrameError {
		t.Fatalf("expected receiver to reject invalid stream token, got %v", bf.Type)
	}
}

func TestAdversarialSourceFileModifiedMidTransfer(t *testing.T) {
	sourceDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "mod.bin")
	_ = os.WriteFile(filePath, bytes.Repeat([]byte("A"), 100000), 0644)

	items, totalFiles, totalBytes, manifestHash, err := ScanSourceItems([]string{sourceDir})
	if err != nil {
		t.Fatal(err)
	}

	receiverID, _ := identity.GenerateNewIdentity()
	senderID, _ := identity.GenerateNewIdentity()
	st, _ := store.OpenStore(":memory:")
	defer st.Close()

	_ = st.SaveTrustedDevice(store.TrustedDevice{
		Fingerprint: senderID.Fingerprint, DeviceID: senderID.DeviceID,
		Name: "ModSender", Platform: "windows",
		PairedAt: time.Now().UnixMilli(), LastSeen: time.Now().UnixMilli(),
	})

	destDir := t.TempDir()
	addr, _, closeListener := startTestReceiver(t, receiverID, st, destDir, false)
	defer closeListener()

	// Modify file before sender starts reading chunks
	_ = os.WriteFile(filePath, bytes.Repeat([]byte("B"), 150000), 0644)

	_, err = StartSend(
		addr, senderID, "ModSender", "mod-test-01", items, totalFiles, totalBytes,
		manifestHash, 65536, nil, receiverID.Fingerprint,
	)
	if err == nil {
		t.Fatal("expected StartSend to fail when source file is modified")
	}
	t.Logf("StartSend correctly failed on modified file: %v", err)
}

func TestAdversarialReceiverMismatchedFingerprint(t *testing.T) {
	sourceDir := t.TempDir()
	filePath := filepath.Join(sourceDir, "test.bin")
	_ = os.WriteFile(filePath, []byte("adversarial payload"), 0644)

	items, totalFiles, totalBytes, manifestHash, err := ScanSourceItems([]string{sourceDir})
	if err != nil {
		t.Fatal(err)
	}

	receiverID, _ := identity.GenerateNewIdentity()
	senderID, _ := identity.GenerateNewIdentity()
	attackerID, _ := identity.GenerateNewIdentity()
	st, _ := store.OpenStore(":memory:")
	defer st.Close()

	destDir := t.TempDir()
	addr, _, closeListener := startTestReceiver(t, receiverID, st, destDir, false)
	defer closeListener()

	// Sender pins attackerID's fingerprint instead of receiverID's fingerprint
	_, err = StartSend(
		addr, senderID, "PinSender", "pin-test-01", items, totalFiles, totalBytes,
		manifestHash, 65536, nil, attackerID.Fingerprint,
	)
	if err == nil {
		t.Fatal("expected StartSend to fail with fingerprint mismatch")
	}
	t.Logf("StartSend correctly rejected mismatched fingerprint: %v", err)
}

func TestAdversarialTransferDeclinedSentinel(t *testing.T) {
	receiverID, _ := identity.GenerateNewIdentity()
	senderID, _ := identity.GenerateNewIdentity()
	st, _ := store.OpenStore(":memory:")
	defer st.Close()

	_ = st.SaveTrustedDevice(store.TrustedDevice{
		Fingerprint: senderID.Fingerprint, DeviceID: senderID.DeviceID,
		Name: "DeclinedSender", Platform: "windows",
		PairedAt: time.Now().UnixMilli(), LastSeen: time.Now().UnixMilli(),
	})

	listener, err := tls.Listen("tcp", "127.0.0.1:0", receiverID.ServerTLSConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	onDecision := func(offer protocol.OfferMessage, peerName, peerFP string) (bool, string) {
		return false, ""
	}

	errChan := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer conn.Close()
		errChan <- HandleIncomingControl(conn, receiverID, st, onDecision, listener)
	}()

	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "data.txt"), []byte("sample"), 0644)
	items, totalFiles, totalBytes, manifestHash, _ := ScanSourceItems([]string{sourceDir})

	// Sender tries to send, receiver declines in onDecision
	_, _ = StartSend(
		listener.Addr().String(), senderID, "DeclinedSender", "dec-test-01", items, totalFiles, totalBytes,
		manifestHash, 65536, nil, receiverID.Fingerprint,
	)

	rxErr := <-errChan
	if !errors.Is(rxErr, ErrTransferDeclined) {
		t.Fatalf("expected ErrTransferDeclined, got: %v", rxErr)
	}
}

func TestAdversarialPairingSuccessSentinel(t *testing.T) {
	receiverID, _ := identity.GenerateNewIdentity()
	senderID, _ := identity.GenerateNewIdentity()
	st, _ := store.OpenStore(":memory:")
	defer st.Close()

	pm := pairing.NewManager()
	tokenBytes, _ := pm.CreateNewToken()
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	SetPairingManager(pm)

	destDir := t.TempDir()
	addr, errChan, closeListener := startTestReceiver(t, receiverID, st, destDir, false)
	defer closeListener()

	rawConn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer rawConn.Close()

	tlsConn := tls.Client(rawConn, senderID.ClientTLSConfigPinned(receiverID.Fingerprint))
	if err := tlsConn.Handshake(); err != nil {
		t.Fatal(err)
	}

	hello := protocol.HelloMessage{
		Version:  protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
		DeviceID: senderID.DeviceID,
		Name:     "PairingSender",
		Platform: "windows",
		Role:     "control",
	}
	_ = protocol.WriteJSONFrame(tlsConn, protocol.FrameHello, hello)
	_, _ = protocol.ReadFrame(tlsConn)

	pairReq := protocol.PairRequestMessage{
		Token:    token,
		Name:     "PairingSender",
		Platform: "windows",
	}
	_ = protocol.WriteJSONFrame(tlsConn, protocol.FramePairRequest, pairReq)
	_, _ = protocol.ReadFrame(tlsConn)

	rxErr := <-errChan
	if !errors.Is(rxErr, ErrPairingSuccess) {
		t.Fatalf("expected ErrPairingSuccess, got: %v", rxErr)
	}
}
