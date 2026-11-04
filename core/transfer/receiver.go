package transfer

import (
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"lanvault/core/fsutil"
	"lanvault/core/identity"
	"lanvault/core/pairing"
	"lanvault/core/protocol"
	"lanvault/core/store"
)

var (
	globalPairingMgrMu sync.RWMutex
	globalPairingMgr   *pairing.Manager

	// controlSlots bounds the number of concurrent TLS handshakes and sessions
	// per process (MAX_CONTROL_CONNECTIONS).
	controlSlots = make(chan struct{}, protocol.MaxControlConnections)

	errNeedRetry = errors.New("chunk must be re-requested before finalization")
	errNotFinal  = errors.New("file is not fully received")

	// ErrPairingSuccess indicates that an incoming control connection was a successful pairing handshake.
	ErrPairingSuccess = errors.New("pairing successful")
	// ErrTransferDeclined indicates that an incoming offer was declined by the user or timed out.
	ErrTransferDeclined = errors.New("transfer declined by user")
	// ErrTransferCancelled indicates that a transfer was cancelled by the peer or user.
	ErrTransferCancelled = errors.New("transfer cancelled")
)

// SetPairingManager configures the active pairing manager for handling PAIR_REQUEST frames
func SetPairingManager(pm *pairing.Manager) {
	globalPairingMgrMu.Lock()
	defer globalPairingMgrMu.Unlock()
	globalPairingMgr = pm
}

// DecisionCallback is called when an OFFER arrives to decide whether to accept.
// It must return promptly; the protocol allows up to OFFER_RESPONSE_TIMEOUT_S.
type DecisionCallback func(offer protocol.OfferMessage, peerName, peerFP string) (accept bool, targetDir string)

// ReceiverSession manages a single incoming transfer on the receiving end
type ReceiverSession struct {
	TransferID     string
	SenderFP       string
	SenderName     string
	Offer          protocol.OfferMessage
	Entries        []protocol.ManifestEntry
	PlannedPaths   []string
	TargetDir      string
	ChunkSize      int64
	StreamToken    string
	DataStreams    int
	Bitmaps        map[int]*protocol.Bitmap // file_index -> Bitmap
	FilesCompleted int
	BytesReceived  int64

	store       *store.Store
	controlConn net.Conn
	ctrlMu      sync.Mutex
	mu          sync.Mutex

	partFiles    map[int]*os.File
	partPaths    map[int]string
	finalPaths   map[int]string
	alreadyFinal map[int]bool
	finalized    map[int]bool
	priorChunks  map[int]map[int]bool
	needAttempts map[string]int
	failedFiles  map[int]string

	createdAt    int64
	doneReceived bool
	donePending  bool
	completing   bool
	terminal     bool
	cancelled    bool
	doneOk       *protocol.DoneOkMessage
}

var (
	activeSessionsMu sync.Mutex
	activeSessions   = make(map[string]*ReceiverSession)
)

func registerActiveSession(sess *ReceiverSession) {
	activeSessionsMu.Lock()
	defer activeSessionsMu.Unlock()
	activeSessions[sess.TransferID] = sess
}

func unregisterActiveSession(sess *ReceiverSession) {
	activeSessionsMu.Lock()
	defer activeSessionsMu.Unlock()
	if activeSessions[sess.TransferID] == sess {
		delete(activeSessions, sess.TransferID)
	}
}

func getActiveSession(transferID string) *ReceiverSession {
	activeSessionsMu.Lock()
	defer activeSessionsMu.Unlock()
	return activeSessions[transferID]
}

func writeWireError(conn net.Conn, code protocol.WireErrorCode, msg string, fatal bool) {
	_ = protocol.WriteJSONFrame(conn, protocol.FrameError, protocol.ErrorMessage{
		Code:    int(code),
		Message: msg,
		Fatal:   fatal,
	})
}

func writeReject(conn net.Conn, transferID string, code protocol.WireErrorCode, msg string) {
	_ = protocol.WriteJSONFrame(conn, protocol.FrameReject, protocol.RejectMessage{
		TransferID: transferID,
		Code:       int(code),
		Message:    msg,
	})
}

func allowUnpairedSenders(st *store.Store) bool {
	if st == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(st.GetSetting("receiving.allow_unpaired", "false"))) {
	case "true", "1", "on", "yes":
		return true
	default:
		return false
	}
}

func isTerminalTransferState(state string) bool {
	switch state {
	case "Completed", "CompletedWithErrors", "Cancelled", "Failed":
		return true
	default:
		return false
	}
}

func chunksForSize(size, chunkSize int64) int {
	if size <= 0 || chunkSize <= 0 {
		return 0
	}
	return int((size + chunkSize - 1) / chunkSize)
}

func chunkLen(size, chunkSize int64, chunkIndex int) int64 {
	offset := int64(chunkIndex) * chunkSize
	remaining := size - offset
	if remaining < chunkSize {
		return remaining
	}
	return chunkSize
}

// HandleIncomingControl handles an incoming control or data connection from a sender.
func HandleIncomingControl(
	controlConn net.Conn,
	serverID *identity.Identity,
	st *store.Store,
	onDecision DecisionCallback,
	dataListener net.Listener,
) error {
	select {
	case controlSlots <- struct{}{}:
		defer func() { <-controlSlots }()
	default:
		return errors.New("too many concurrent connections")
	}

	tlsConn, ok := controlConn.(*tls.Conn)
	if !ok {
		return errors.New("control connection must be TLS")
	}
	if serverID == nil {
		return errors.New("receiver identity is required")
	}

	_ = tlsConn.SetDeadline(time.Now().Add(time.Duration(protocol.HelloTimeoutMs) * time.Millisecond))
	if err := tlsConn.Handshake(); err != nil {
		return fmt.Errorf("server TLS handshake failed: %w", err)
	}

	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return errors.New("missing client certificate")
	}
	peerFP := identity.FingerprintFromDER(state.PeerCertificates[0].Raw)
	if peerFP == "" {
		return errors.New("empty peer fingerprint")
	}

	frame, err := protocol.ReadFrame(tlsConn)
	if err != nil {
		return fmt.Errorf("read HELLO failed: %w", err)
	}
	if frame.Type != protocol.FrameHello {
		writeWireError(tlsConn, protocol.ErrCodeProtocolError, "expected HELLO", true)
		return fmt.Errorf("expected HELLO, got %s", frame.Type)
	}
	hello, err := protocol.DecodeJSONPayload[protocol.HelloMessage](frame.Payload)
	if err != nil {
		writeWireError(tlsConn, protocol.ErrCodeProtocolError, "malformed HELLO", true)
		return fmt.Errorf("decode HELLO failed: %w", err)
	}
	if hello.Version.Major != protocol.ProtocolMajor {
		writeWireError(tlsConn, protocol.ErrCodeUnsupportedVersion, "Unsupported wire protocol major version", true)
		return errors.New("unsupported protocol major version")
	}
	if hello.Role != "control" && hello.Role != "data" {
		writeWireError(tlsConn, protocol.ErrCodeProtocolError, "invalid HELLO role", true)
		return fmt.Errorf("invalid HELLO role %q", hello.Role)
	}
	if len(hello.Name) > 256 {
		writeWireError(tlsConn, protocol.ErrCodeProtocolError, "device name too long", true)
		return errors.New("device name too long")
	}
	// device_id is derived from the certificate and cannot be chosen freely.
	expectedDeviceID := identity.DeviceIDFromFingerprint(peerFP)
	if hello.DeviceID == "" || !strings.EqualFold(hello.DeviceID, expectedDeviceID) {
		writeWireError(tlsConn, protocol.ErrCodeNotPaired,
			"device_id does not match the presented certificate", true)
		return errors.New("device_id/fingerprint mismatch")
	}
	_ = tlsConn.SetDeadline(time.Time{})

	paired := false
	if st != nil {
		if _, err := st.GetTrustedDevice(peerFP); err == nil {
			paired = true
		}
	}

	helloOk := protocol.HelloOkMessage{
		Version:      protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
		DeviceID:     serverID.DeviceID,
		Name:         "LANVault Receiver",
		Platform:     runtime.GOOS,
		Capabilities: []string{"resume", "multi-stream"},
		Paired:       paired,
	}

	if hello.Role == "data" {
		return handleDataConnection(tlsConn, hello, helloOk, peerFP)
	}

	if err := protocol.WriteJSONFrame(tlsConn, protocol.FrameHelloOk, helloOk); err != nil {
		return fmt.Errorf("write HELLO_OK failed: %w", err)
	}

	_ = tlsConn.SetReadDeadline(time.Now().Add(time.Duration(protocol.OfferResponseTimeoutSec) * time.Second))
	frame, err = protocol.ReadFrame(tlsConn)
	if err != nil {
		return fmt.Errorf("read control frame failed: %w", err)
	}
	_ = tlsConn.SetReadDeadline(time.Time{})

	switch frame.Type {
	case protocol.FramePairRequest:
		if err := handlePairRequest(tlsConn, frame.Payload, hello, helloOk, peerFP, st, paired); err != nil {
			return err
		}
		return ErrPairingSuccess
	case protocol.FrameOffer:
		// handled below
	default:
		writeWireError(tlsConn, protocol.ErrCodeProtocolError, "expected OFFER or PAIR_REQUEST", true)
		return fmt.Errorf("expected OFFER or PAIR_REQUEST, got %s", frame.Type)
	}

	offer, err := protocol.DecodeJSONPayload[protocol.OfferMessage](frame.Payload)
	if err != nil {
		writeWireError(tlsConn, protocol.ErrCodeProtocolError, "malformed OFFER", true)
		return fmt.Errorf("decode OFFER failed: %w", err)
	}
	if err := validateOffer(offer); err != nil {
		if errors.Is(err, errPathInvalid) {
			writeReject(tlsConn, offer.TransferID, protocol.ErrCodePathInvalid, err.Error())
		} else {
			writeWireError(tlsConn, protocol.ErrCodeProtocolError, err.Error(), true)
		}
		return err
	}

	// Unpaired peers may only reach OFFER when explicitly permitted, or when
	// this is a resume of a transfer that was already accepted from the same
	// certificate.
	var existing *store.TransferRecord
	resumeCandidate := false
	if st != nil {
		if rec, getErr := st.GetTransfer(offer.TransferID); getErr == nil && rec != nil {
			existing = rec
			if rec.PeerFingerprint == peerFP && !isTerminalTransferState(rec.State) {
				resumeCandidate = true
			}
		}
	}
	if !paired && !allowUnpairedSenders(st) && !resumeCandidate {
		writeWireError(tlsConn, protocol.ErrCodeNotPaired,
			"receiver does not allow unpaired senders; pair first", true)
		return errors.New("offer rejected: sender is not paired")
	}

	entries, manifestHash, err := readManifest(tlsConn, offer)
	if err != nil {
		return err
	}

	plannedPaths, rejectReason, planErr := fsutil.PlanManifest(manifestInputs(entries), "windows")
	if planErr != nil || rejectReason != "" {
		writeReject(tlsConn, offer.TransferID, protocol.ErrCodePathInvalid,
			fmt.Sprintf("invalid path or namespace conflict: %s", rejectReason))
		if planErr != nil {
			return planErr
		}
		return fmt.Errorf("manifest rejected: %s", rejectReason)
	}

	// Bind resume state to (transfer_id, sender fingerprint, manifest_hash,
	// chunk_size). Never expose state belonging to a different binding.
	existingFiles := make(map[int]store.TransferFileRecord)
	isResume := false
	var createdAt int64
	if existing != nil {
		sameBinding := existing.PeerFingerprint == peerFP &&
			existing.ManifestHash == manifestHash &&
			existing.ChunkSize == offer.ChunkSize
		if existing.PeerFingerprint != peerFP {
			writeWireError(tlsConn, protocol.ErrCodeBusy,
				"transfer_id is already in use by another device", true)
			return errors.New("transfer_id collision with another device")
		}
		if isTerminalTransferState(existing.State) {
			writeWireError(tlsConn, protocol.ErrCodeBusy,
				"transfer_id already refers to a finished transfer", true)
			return errors.New("transfer_id already finished")
		}
		if sameBinding {
			isResume = true
			createdAt = existing.CreatedAt
			files, fileErr := st.GetTransferFiles(offer.TransferID)
			if fileErr != nil {
				return fmt.Errorf("load resume state failed: %w", fileErr)
			}
			for _, f := range files {
				existingFiles[f.FileIndex] = f
			}
		} else {
			// Same peer, changed manifest: old state cannot be reused. It is
			// safe to discard because the binding proves the same certificate.
			if err := st.DeleteTransfer(offer.TransferID); err != nil {
				return fmt.Errorf("discard stale transfer state failed: %w", err)
			}
		}
	}

	// Destination selection. Resume does not re-prompt because this exact
	// transfer was already accepted from this certificate.
	var targetDir string
	if isResume {
		targetDir = existingTargetDir(st, offer.TransferID)
	}
	if targetDir == "" {
		if onDecision == nil {
			writeReject(tlsConn, offer.TransferID, protocol.ErrCodeRejected, "no receiver decision handler")
			return errors.New("no decision handler")
		}
		accept, dir := onDecision(*offer, hello.Name, peerFP)
		if !accept {
			writeReject(tlsConn, offer.TransferID, protocol.ErrCodeRejected, "Declined by user")
			return ErrTransferDeclined
		}
		targetDir = dir
	}
	if strings.TrimSpace(targetDir) == "" {
		writeReject(tlsConn, offer.TransferID, protocol.ErrCodePathInvalid, "destination directory is required")
		return errors.New("empty destination directory")
	}
	targetRoot, err := prepareDestination(targetDir)
	if err != nil {
		writeReject(tlsConn, offer.TransferID, protocol.ErrCodePathInvalid, err.Error())
		return err
	}
	if st != nil {
		// Persist the accepted destination so a resume never silently changes
		// where files are written and never needs to re-prompt.
		_ = st.SetSetting("transfer."+offer.TransferID+".target_dir", targetRoot)
	}

	heldBytes := heldResumeBytes(existingFiles, entries, offer.ChunkSize, isResume)
	if free, freeErr := fsutil.FreeBytes(targetRoot); freeErr == nil {
		need := offer.TotalBytes - heldBytes
		if need < 0 {
			need = 0
		}
		if free < need {
			writeReject(tlsConn, offer.TransferID, protocol.ErrCodeDiskFull,
				fmt.Sprintf("destination needs %d bytes but only %d are free", need, free))
			return errors.New("destination disk full")
		}
	}
	maxFileSize := fsutil.MaxFileSize(targetRoot)
	for _, e := range entries {
		if e.Type == "file" && e.Size > maxFileSize {
			writeReject(tlsConn, offer.TransferID, protocol.ErrCodeFileTooLargeForFS,
				fmt.Sprintf("file %s is %d bytes, larger than the destination filesystem limit", e.Path, e.Size))
			return fmt.Errorf("file %s too large for destination filesystem", e.Path)
		}
	}

	dataStreams := offer.DataStreams
	if dataStreams <= 0 || dataStreams > protocol.DataStreamsMax {
		dataStreams = protocol.DataStreamsDefault
	}
	if dataStreams > protocol.DataStreamsMax {
		dataStreams = protocol.DataStreamsMax
	}

	token, err := newStreamToken()
	if err != nil {
		return err
	}

	sess := &ReceiverSession{
		TransferID:   offer.TransferID,
		SenderFP:     peerFP,
		SenderName:   hello.Name,
		Offer:        *offer,
		Entries:      entries,
		PlannedPaths: plannedPaths,
		TargetDir:    targetRoot,
		ChunkSize:    offer.ChunkSize,
		StreamToken:  token,
		DataStreams:  dataStreams,
		Bitmaps:      make(map[int]*protocol.Bitmap),
		store:        st,
		controlConn:  tlsConn,
		partFiles:    make(map[int]*os.File),
		partPaths:    make(map[int]string),
		finalPaths:   make(map[int]string),
		alreadyFinal: make(map[int]bool),
		finalized:    make(map[int]bool),
		priorChunks:  make(map[int]map[int]bool),
		needAttempts: make(map[string]int),
		failedFiles:  make(map[int]string),
		createdAt:    createdAt,
	}
	if sess.createdAt == 0 {
		sess.createdAt = time.Now().UnixMilli()
	}

	have, initErr := sess.initialize(isResume, existingFiles)
	if initErr != nil {
		sess.cleanup()
		writeReject(tlsConn, offer.TransferID, protocol.ErrCodePathInvalid, initErr.Error())
		return fmt.Errorf("initialize destination failed: %w", initErr)
	}

	registerActiveSession(sess)
	defer unregisterActiveSession(sess)
	defer sess.cleanup()

	accept := protocol.AcceptMessage{
		TransferID:  offer.TransferID,
		Resume:      isResume,
		StreamToken: token,
		DataStreams: dataStreams,
		Have:        have,
	}
	_ = tlsConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := protocol.WriteJSONFrame(tlsConn, protocol.FrameAccept, accept); err != nil {
		return fmt.Errorf("write ACCEPT failed: %w", err)
	}
	_ = tlsConn.SetWriteDeadline(time.Time{})
	sess.touch()

	if dataListener != nil {
		go acceptDataConnections(dataListener, serverID, st, onDecision, sess)
	}

	return sess.controlLoop()
}

func acceptDataConnections(listener net.Listener, serverID *identity.Identity, st *store.Store, onDecision DecisionCallback, sess *ReceiverSession) {
	for i := 0; i < sess.DataStreams; i++ {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			_ = HandleIncomingControl(c, serverID, st, onDecision, nil)
		}(conn)
	}
}

func handleDataConnection(tlsConn *tls.Conn, hello *protocol.HelloMessage, helloOk protocol.HelloOkMessage, peerFP string) error {
	if hello.TransferID == "" || hello.StreamToken == "" {
		writeWireError(tlsConn, protocol.ErrCodeProtocolError, "data connection requires transfer_id and stream_token", true)
		return errors.New("data connection missing credentials")
	}
	sess := getActiveSession(hello.TransferID)
	if sess == nil {
		writeWireError(tlsConn, protocol.ErrCodeBusy, "unknown or expired transfer session", true)
		return errors.New("unknown transfer session")
	}
	if sess.SenderFP != peerFP {
		writeWireError(tlsConn, protocol.ErrCodeNotPaired, "data connection certificate does not match control session", true)
		return errors.New("data connection fingerprint mismatch")
	}
	if subtle.ConstantTimeCompare([]byte(sess.StreamToken), []byte(hello.StreamToken)) != 1 {
		writeWireError(tlsConn, protocol.ErrCodeProtocolError, "invalid stream token", true)
		return errors.New("invalid stream token")
	}
	if sess.isTerminal() {
		writeWireError(tlsConn, protocol.ErrCodeStateIntegrity, "transfer session already finished", true)
		return errors.New("transfer session finished")
	}

	sess.touch()
	if err := protocol.WriteJSONFrame(tlsConn, protocol.FrameHelloOk, helloOk); err != nil {
		return err
	}

	deadline := time.Duration(protocol.DeadPeerTimeoutMs) * time.Millisecond
	for {
		_ = tlsConn.SetReadDeadline(time.Now().Add(deadline))
		f, err := protocol.ReadFrame(tlsConn)
		if err != nil {
			if sess.isTerminal() {
				return nil
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				sess.noteError("data connection timed out")
			}
			return err
		}
		sess.touch()

		switch f.Type {
		case protocol.FrameData:
			if err := sess.handleDataFrame(f.Payload); err != nil {
				if sess.isTerminal() {
					return nil
				}
				slog.Warn("data frame rejected", "transfer", sess.TransferID, "err", err)
			}
		case protocol.FramePing:
			if err := protocol.WriteRawFrame(tlsConn, protocol.FramePong, nil); err != nil {
				return err
			}
		case protocol.FramePong:
			// keepalive reply
		case protocol.FrameDone:
			if err := sess.handleDoneFrame(f.Payload); err != nil {
				return err
			}
			return nil
		case protocol.FrameCancel:
			sess.cancel()
			return nil
		case protocol.FrameError:
			msg, decErr := protocol.DecodeJSONPayload[protocol.ErrorMessage](f.Payload)
			if decErr == nil && msg.Fatal {
				sess.noteError(msg.Message)
				return fmt.Errorf("peer error: %s", msg.Message)
			}
		default:
			writeWireError(tlsConn, protocol.ErrCodeProtocolError, "unexpected frame on data connection", true)
			sess.noteError("unexpected frame on data connection")
			return fmt.Errorf("unexpected frame %s on data connection", f.Type)
		}
	}
}

func handlePairRequest(
	tlsConn *tls.Conn,
	payload []byte,
	hello *protocol.HelloMessage,
	helloOk protocol.HelloOkMessage,
	peerFP string,
	st *store.Store,
	alreadyPaired bool,
) error {
	pairReq, err := protocol.DecodeJSONPayload[protocol.PairRequestMessage](payload)
	if err != nil {
		writeWireError(tlsConn, protocol.ErrCodeProtocolError, "malformed PAIR_REQUEST", true)
		return err
	}
	globalPairingMgrMu.RLock()
	pm := globalPairingMgr
	globalPairingMgrMu.RUnlock()
	if pm == nil {
		writeWireError(tlsConn, protocol.ErrCodePairTokenInvalid, "pairing is not active on this receiver", true)
		return errors.New("pairing not active")
	}
	if err := pm.VerifyToken(pairReq.Token); err != nil {
		writeWireError(tlsConn, protocol.ErrCodePairTokenInvalid, fmt.Sprintf("pairing token invalid or expired: %v", err), true)
		return fmt.Errorf("pairing token verification failed: %w", err)
	}
	if st != nil {
		now := time.Now().UnixMilli()
		dev := store.TrustedDevice{
			Fingerprint: peerFP,
			DeviceID:    hello.DeviceID,
			Name:        pairReq.Name,
			Platform:    pairReq.Platform,
			PairedAt:    now,
			LastSeen:    now,
		}
		if alreadyPaired {
			if old, err := st.GetTrustedDevice(peerFP); err == nil {
				dev.PairedAt = old.PairedAt
				dev.AutoAccept = old.AutoAccept
			}
		}
		if err := st.SaveTrustedDevice(dev); err != nil {
			return fmt.Errorf("persist trusted device failed: %w", err)
		}
	}
	return protocol.WriteJSONFrame(tlsConn, protocol.FramePairOk, protocol.PairOkMessage{
		DeviceID: helloOk.DeviceID,
		Name:     helloOk.Name,
	})
}

var errPathInvalid = errors.New("invalid manifest")

func validateOffer(offer *protocol.OfferMessage) error {
	if offer == nil {
		return errors.New("missing offer")
	}
	if offer.TransferID == "" || len(offer.TransferID) > 128 {
		return fmt.Errorf("%w: invalid transfer_id", errPathInvalid)
	}
	if offer.ChunkSize < protocol.ChunkSizeMin || offer.ChunkSize > protocol.ChunkSizeMax {
		return fmt.Errorf("chunk_size %d out of bounds [%d,%d]", offer.ChunkSize, protocol.ChunkSizeMin, protocol.ChunkSizeMax)
	}
	if offer.FileCount < 0 || offer.DirCount < 0 ||
		offer.FileCount > protocol.MaxManifestEntries ||
		offer.DirCount > protocol.MaxManifestEntries ||
		offer.FileCount+offer.DirCount > protocol.MaxManifestEntries {
		return fmt.Errorf("%w: manifest entry count out of bounds", errPathInvalid)
	}
	if offer.TotalBytes < 0 {
		return errors.New("total_bytes cannot be negative")
	}
	if offer.DataStreams < 0 || offer.DataStreams > protocol.DataStreamsMax {
		return fmt.Errorf("data_streams %d out of bounds", offer.DataStreams)
	}
	return nil
}

func manifestInputs(entries []protocol.ManifestEntry) []fsutil.EntryInput {
	inputs := make([]fsutil.EntryInput, len(entries))
	for i, e := range entries {
		inputs[i] = fsutil.EntryInput{Type: e.Type, Path: e.Path}
	}
	return inputs
}

func readManifest(conn net.Conn, offer *protocol.OfferMessage) ([]protocol.ManifestEntry, string, error) {
	var entries []protocol.ManifestEntry
	var end *protocol.ManifestEndMessage
	deadline := time.Duration(protocol.OfferResponseTimeoutSec) * time.Second

	for {
		_ = conn.SetReadDeadline(time.Now().Add(deadline))
		f, err := protocol.ReadFrame(conn)
		if err != nil {
			writeWireError(conn, protocol.ErrCodeTimeout, "manifest read timed out or failed", true)
			return nil, "", fmt.Errorf("read manifest failed: %w", err)
		}
		switch f.Type {
		case protocol.FrameManifestPart:
			part, err := protocol.DecodeJSONPayload[protocol.ManifestPartMessage](f.Payload)
			if err != nil {
				writeWireError(conn, protocol.ErrCodeProtocolError, "malformed MANIFEST_PART", true)
				return nil, "", err
			}
			if len(part.Entries) == 0 {
				writeWireError(conn, protocol.ErrCodeProtocolError, "empty MANIFEST_PART", true)
				return nil, "", errors.New("empty manifest part")
			}
			if len(part.Entries) > protocol.ManifestPartMaxEntries {
				writeReject(conn, offer.TransferID, protocol.ErrCodePathInvalid, "MANIFEST_PART exceeds entry limit")
				return nil, "", errors.New("manifest part too large")
			}
			entries = append(entries, part.Entries...)
			if len(entries) > protocol.MaxManifestEntries {
				writeReject(conn, offer.TransferID, protocol.ErrCodePathInvalid, "manifest exceeds maximum entry count")
				return nil, "", errors.New("manifest too large")
			}
		case protocol.FrameManifestEnd:
			endMsg, err := protocol.DecodeJSONPayload[protocol.ManifestEndMessage](f.Payload)
			if err != nil {
				writeWireError(conn, protocol.ErrCodeProtocolError, "malformed MANIFEST_END", true)
				return nil, "", err
			}
			end = endMsg
		case protocol.FrameError:
			msg, _ := protocol.DecodeJSONPayload[protocol.ErrorMessage](f.Payload)
			if msg != nil {
				return nil, "", fmt.Errorf("peer error during manifest: %s", msg.Message)
			}
			return nil, "", errors.New("peer error during manifest")
		default:
			writeWireError(conn, protocol.ErrCodeProtocolError, "unexpected frame during manifest", true)
			return nil, "", fmt.Errorf("unexpected frame during manifest: %s", f.Type)
		}
		if end != nil {
			break
		}
	}
	_ = conn.SetReadDeadline(time.Time{})

	if end.EntryCount != len(entries) {
		writeWireError(conn, protocol.ErrCodeProtocolError,
			fmt.Sprintf("MANIFEST_END entry_count %d does not match received %d", end.EntryCount, len(entries)), true)
		return nil, "", errors.New("manifest entry count mismatch")
	}
	if err := protocol.ValidateManifestIndices(entries); err != nil {
		writeWireError(conn, protocol.ErrCodeProtocolError, err.Error(), true)
		return nil, "", err
	}

	files, dirs := 0, 0
	var totalBytes int64
	for _, e := range entries {
		switch e.Type {
		case "file":
			if e.Size < 0 {
				writeReject(conn, offer.TransferID, protocol.ErrCodePathInvalid, "negative file size")
				return nil, "", errors.New("negative file size")
			}
			files++
			totalBytes += e.Size
		case "dir":
			if e.Size != 0 {
				writeReject(conn, offer.TransferID, protocol.ErrCodePathInvalid, "directory entry has non-zero size")
				return nil, "", errors.New("directory with size")
			}
			dirs++
		default:
			writeReject(conn, offer.TransferID, protocol.ErrCodePathInvalid, "invalid entry type")
			return nil, "", fmt.Errorf("invalid entry type %q", e.Type)
		}
	}
	if files != offer.FileCount || dirs != offer.DirCount {
		writeWireError(conn, protocol.ErrCodeProtocolError, "manifest counts do not match OFFER", true)
		return nil, "", errors.New("manifest counts do not match offer")
	}
	if totalBytes != offer.TotalBytes {
		writeWireError(conn, protocol.ErrCodeProtocolError, "manifest total_bytes does not match OFFER", true)
		return nil, "", errors.New("manifest bytes do not match offer")
	}

	hash := protocol.ComputeManifestHash(entries)
	if !strings.EqualFold(hash, end.ManifestHash) {
		writeWireError(conn, protocol.ErrCodeHashMismatch, "manifest hash mismatch", true)
		return nil, "", errors.New("manifest hash mismatch")
	}
	return entries, hash, nil
}

func prepareDestination(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("destination directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("destination is not a directory: %s", abs)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs), nil
}

func existingTargetDir(st *store.Store, transferID string) string {
	if st == nil {
		return ""
	}
	return strings.TrimSpace(st.GetSetting("transfer."+transferID+".target_dir", ""))
}

// heldResumeBytes counts bytes already represented by persisted, hash-backed
// state. It is used only for the pre-accept free-space check.
func heldResumeBytes(files map[int]store.TransferFileRecord, entries []protocol.ManifestEntry, chunkSize int64, isResume bool) int64 {
	if !isResume {
		return 0
	}
	var held int64
	for _, e := range entries {
		if e.Type != "file" || e.Size == 0 {
			continue
		}
		rec, ok := files[e.Index]
		if !ok || rec.Size != e.Size {
			continue
		}
		bmp, err := protocol.FromBytes(chunksForSize(e.Size, chunkSize), rec.ChunkBitmap)
		if err != nil {
			continue
		}
		for c := 0; c < bmp.ChunkCount(); c++ {
			if bmp.Has(c) {
				held += chunkLen(e.Size, chunkSize, c)
			}
		}
	}
	return held
}

func (s *ReceiverSession) initialize(isResume bool, existing map[int]store.TransferFileRecord) (map[string]string, error) {
	have := make(map[string]string)
	var records []store.TransferFileRecord

	for i, e := range s.Entries {
		rel := s.PlannedPaths[i]
		full, err := fsutil.SafeJoin(s.TargetDir, rel)
		if err != nil {
			return nil, fmt.Errorf("unsafe path %q: %w", rel, err)
		}

		if e.Type == "dir" {
			if err := os.MkdirAll(full, 0755); err != nil {
				return nil, fmt.Errorf("create directory %q failed: %w", rel, err)
			}
			records = append(records, store.TransferFileRecord{
				TransferID: s.TransferID,
				FileIndex:  e.Index,
				Type:       e.Type,
				WirePath:   e.Path,
				FinalPath:  rel,
				Size:       0,
				MtimeMs:    e.MtimeMs,
				ChunkCount: 0,
			})
			continue
		}

		chunkCount := chunksForSize(e.Size, s.ChunkSize)
		var bmp *protocol.Bitmap
		if isResume {
			if rec, ok := existing[e.Index]; ok && rec.Size == e.Size && rec.ChunkCount == chunkCount {
				if parsed, err := protocol.FromBytes(chunkCount, rec.ChunkBitmap); err == nil {
					bmp = parsed
				}
			}
		}
		if bmp == nil {
			bmp = protocol.NewBitmap(chunkCount)
		}
		s.Bitmaps[e.Index] = bmp
		s.partPaths[e.Index] = full + ".lvpart"
		s.finalPaths[e.Index] = full

		// A previous session may have finalized this file. It can only be
		// trusted if every chunk has a persisted hash to verify against.
		if _, statErr := os.Lstat(full); statErr == nil && bmp.IsComplete() && e.Size > 0 {
			hashes, hashErr := s.loadHashes(e.Index)
			if hashErr == nil && len(hashes) == chunkCount {
				s.alreadyFinal[e.Index] = true
				s.priorChunks[e.Index] = make(map[int]bool, chunkCount)
				for c := 0; c < chunkCount; c++ {
					s.priorChunks[e.Index][c] = true
				}
				s.BytesReceived += e.Size
				have[strconv.Itoa(e.Index)] = bmp.ToBase64()
				records = append(records, store.TransferFileRecord{
					TransferID:  s.TransferID,
					FileIndex:   e.Index,
					Type:        e.Type,
					WirePath:    e.Path,
					FinalPath:   rel,
					Size:        e.Size,
					MtimeMs:     e.MtimeMs,
					ChunkCount:  chunkCount,
					ChunkBitmap: bmp.Bytes(),
				})
				continue
			}
			// Cannot verify the existing final file. Leave it untouched and
			// re-receive into a partial file; finalization will choose a free
			// numbered name instead of overwriting.
			bmp = protocol.NewBitmap(chunkCount)
			s.Bitmaps[e.Index] = bmp
		}

		if _, err := os.Lstat(s.partPaths[e.Index]); err == nil {
			info, lerr := os.Lstat(s.partPaths[e.Index])
			if lerr == nil && info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("partial file is a symlink: %s", s.partPaths[e.Index])
			}
		}

		f, err := openPartialFile(s.partPaths[e.Index])
		if err != nil {
			return nil, err
		}

		// Resume sanity: only keep bits whose chunk lies wholly inside the
		// partial file's current length and whose hash was persisted.
		var prior map[int]bool
		if isResume && bmp.CompletedCount() > 0 {
			hashes, hashErr := s.loadHashes(e.Index)
			if hashErr == nil && len(hashes) > 0 {
				info, statErr := f.Stat()
				if statErr != nil {
					f.Close()
					return nil, statErr
				}
				onDisk := info.Size()
				for c := 0; c < chunkCount; c++ {
					if !bmp.Has(c) {
						continue
					}
					end := int64(c+1) * s.ChunkSize
					if end > e.Size {
						end = e.Size
					}
					if end > onDisk {
						_ = bmp.Clear(c)
						continue
					}
					if _, ok := hashes[c]; !ok {
						_ = bmp.Clear(c)
						continue
					}
					if prior == nil {
						prior = make(map[int]bool)
					}
					prior[c] = true
				}
			} else {
				// No authenticated hash evidence: re-request everything.
				bmp = protocol.NewBitmap(chunkCount)
				s.Bitmaps[e.Index] = bmp
			}
		}
		if len(prior) > 0 {
			s.priorChunks[e.Index] = prior
		}

		var held int64
		for c := 0; c < chunkCount; c++ {
			if bmp.Has(c) {
				held += chunkLen(e.Size, s.ChunkSize, c)
			}
		}
		s.BytesReceived += held
		if bmp.CompletedCount() > 0 {
			have[strconv.Itoa(e.Index)] = bmp.ToBase64()
		}
		_ = f.Sync()
		s.partFiles[e.Index] = f

		records = append(records, store.TransferFileRecord{
			TransferID:  s.TransferID,
			FileIndex:   e.Index,
			Type:        e.Type,
			WirePath:    e.Path,
			FinalPath:   rel,
			Size:        e.Size,
			MtimeMs:     e.MtimeMs,
			ChunkCount:  chunkCount,
			ChunkBitmap: bmp.Bytes(),
		})
	}

	if s.store != nil {
		if err := s.store.SaveTransfer(store.TransferRecord{
			TransferID:      s.TransferID,
			Direction:       "receive",
			PeerFingerprint: s.SenderFP,
			PeerName:        s.SenderName,
			PeerDeviceID:    identity.DeviceIDFromFingerprint(s.SenderFP),
			State:           "Transferring",
			ChunkSize:       s.ChunkSize,
			TotalFiles:      s.Offer.FileCount,
			TotalBytes:      s.Offer.TotalBytes,
			CompletedFiles:  s.FilesCompleted,
			CompletedBytes:  s.BytesReceived,
			ManifestHash:    protocol.ComputeManifestHash(s.Entries),
			CreatedAt:       s.createdAt,
			UpdatedAt:       time.Now().UnixMilli(),
		}); err != nil {
			return nil, fmt.Errorf("save transfer state failed: %w", err)
		}
		if err := s.store.SaveTransferFiles(records); err != nil {
			return nil, fmt.Errorf("save transfer files failed: %w", err)
		}
	}
	return have, nil
}

func openPartialFile(path string) (*os.File, error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("partial file is a symlink: %s", path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0644)
	if err == nil {
		return f, nil
	}
	if errors.Is(err, os.ErrExist) {
		return os.OpenFile(path, os.O_RDWR, 0644)
	}
	return nil, fmt.Errorf("open partial file %s failed: %w", path, err)
}

func (s *ReceiverSession) loadHashes(fileIndex int) (map[int][]byte, error) {
	if s.store == nil {
		return map[int][]byte{}, nil
	}
	return s.store.GetChunkHashes(s.TransferID, fileIndex)
}

func (s *ReceiverSession) touch() {
	if s.controlConn != nil {
		_ = s.controlConn.SetReadDeadline(time.Now().Add(time.Duration(protocol.DeadPeerTimeoutMs) * time.Millisecond))
	}
}

func (s *ReceiverSession) isTerminal() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.terminal
}

func (s *ReceiverSession) writeControlRaw(frameType protocol.FrameType, payload []byte) error {
	s.ctrlMu.Lock()
	defer s.ctrlMu.Unlock()
	_ = s.controlConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return protocol.WriteRawFrame(s.controlConn, frameType, payload)
}

func (s *ReceiverSession) writeControlJSON(frameType protocol.FrameType, v any) error {
	s.ctrlMu.Lock()
	defer s.ctrlMu.Unlock()
	_ = s.controlConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return protocol.WriteJSONFrame(s.controlConn, frameType, v)
}

func (s *ReceiverSession) sendNeed(pairs []protocol.IndexPair) {
	for len(pairs) > 0 {
		n := len(pairs)
		if n > protocol.MaxNeedPairsPerFrame {
			n = protocol.MaxNeedPairsPerFrame
		}
		_ = s.writeControlRaw(protocol.FrameChunkNeed, protocol.EncodeIndexPairs(pairs[:n]))
		pairs = pairs[n:]
	}
}

func (s *ReceiverSession) sendSessionError(code protocol.WireErrorCode, msg string, fileIndex *int, fatal bool) {
	errMsg := protocol.ErrorMessage{Code: int(code), Message: msg, Fatal: fatal}
	if fileIndex != nil {
		errMsg.FileIndex = fileIndex
	}
	_ = s.writeControlJSON(protocol.FrameError, errMsg)
}

func (s *ReceiverSession) controlLoop() error {
	for {
		s.touch()
		f, err := protocol.ReadFrame(s.controlConn)
		if err != nil {
			if s.isTerminal() {
				if s.cancelled {
					return ErrTransferCancelled
				}
				if s.doneOk != nil {
					return nil
				}
				return errors.New("transfer ended without verified completion")
			}
			s.noteError(err.Error())
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				s.sendSessionError(protocol.ErrCodeTimeout, "dead peer: no frames received", nil, true)
			}
			return err
		}
		s.touch()

		switch f.Type {
		case protocol.FramePing:
			if err := s.writeControlRaw(protocol.FramePong, nil); err != nil {
				return err
			}
		case protocol.FramePong:
			// keepalive
		case protocol.FrameDone:
			if err := s.handleDoneFrame(f.Payload); err != nil {
				return err
			}
		case protocol.FrameCancel:
			s.cancel()
			return ErrTransferCancelled
		case protocol.FramePause:
			// Bitmaps are persisted after every chunk, so pausing requires no
			// additional state write. Keep the session open.
		case protocol.FrameResume:
			// Nothing to do; data frames resume naturally.
		case protocol.FrameError:
			msg, decErr := protocol.DecodeJSONPayload[protocol.ErrorMessage](f.Payload)
			if decErr != nil {
				s.noteError("malformed ERROR frame")
				continue
			}
			if msg.Code == int(protocol.ErrCodeFileChanged) && msg.FileIndex != nil {
				s.mu.Lock()
				s.failedFiles[*msg.FileIndex] = msg.Message
				s.mu.Unlock()
			}
			if msg.Fatal {
				s.noteError(msg.Message)
				return fmt.Errorf("peer error (code %d): %s", msg.Code, msg.Message)
			}
		case protocol.FrameChunkAck, protocol.FrameFileDone, protocol.FrameChunkNeed,
			protocol.FrameAccept, protocol.FrameReject, protocol.FrameHello,
			protocol.FrameHelloOk, protocol.FrameOffer, protocol.FrameManifestPart,
			protocol.FrameManifestEnd, protocol.FrameData, protocol.FrameDoneOk:
			s.sendSessionError(protocol.ErrCodeProtocolError, "unexpected frame on control connection", nil, true)
			s.noteError("unexpected control frame")
			return fmt.Errorf("unexpected control frame %s", f.Type)
		default:
			s.sendSessionError(protocol.ErrCodeProtocolError, "unknown frame type", nil, true)
			s.noteError("unknown frame type")
			return fmt.Errorf("unknown frame type %d", f.Type)
		}
	}
}

func (s *ReceiverSession) handleDoneFrame(payload []byte) error {
	done, err := protocol.DecodeJSONPayload[protocol.DoneMessage](payload)
	if err != nil {
		s.sendSessionError(protocol.ErrCodeProtocolError, "malformed DONE", nil, true)
		return err
	}
	if done.TransferID != "" && done.TransferID != s.TransferID {
		s.sendSessionError(protocol.ErrCodeProtocolError, "DONE transfer_id mismatch", nil, true)
		return errors.New("DONE transfer_id mismatch")
	}
	return s.complete()
}

func (s *ReceiverSession) handleDataFrame(payload []byte) error {
	df, err := protocol.DecodeDataFramePayload(payload)
	if err != nil {
		return err
	}
	fileIndex := int(df.FileIndex)
	if fileIndex < 0 || fileIndex >= len(s.Entries) || s.Entries[fileIndex].Type != "file" {
		return fmt.Errorf("invalid file index %d in DATA", fileIndex)
	}
	entry := s.Entries[fileIndex]
	chunkIndex := int(df.ChunkIndex)
	chunkCount := chunksForSize(entry.Size, s.ChunkSize)
	if chunkIndex < 0 || chunkIndex >= chunkCount {
		return fmt.Errorf("chunk index %d out of range for file %d", chunkIndex, fileIndex)
	}
	expectedLen := chunkLen(entry.Size, s.ChunkSize, chunkIndex)
	if int64(len(df.Data)) != expectedLen {
		return fmt.Errorf("chunk length %d does not match expected %d", len(df.Data), expectedLen)
	}
	computed := sha256.Sum256(df.Data)
	if subtle.ConstantTimeCompare(computed[:], df.Hash[:]) != 1 {
		key := fmt.Sprintf("%d:%d", fileIndex, chunkIndex)
		s.mu.Lock()
		s.needAttempts[key]++
		attempts := s.needAttempts[key]
		s.mu.Unlock()
		if attempts >= 3 {
			msg := fmt.Sprintf("chunk %d of file %d failed verification %d times", chunkIndex, fileIndex, attempts)
			s.sendSessionError(protocol.ErrCodeHashMismatch, msg, &fileIndex, true)
			s.mu.Lock()
			s.terminal = true
			s.saveStateLocked("Failed", msg)
			s.mu.Unlock()
			return fmt.Errorf("chunk hash mismatch limit reached")
		}
		s.sendNeed([]protocol.IndexPair{{FileIndex: df.FileIndex, ChunkIndex: df.ChunkIndex}})
		return fmt.Errorf("chunk hash mismatch; re-requested")
	}

	s.mu.Lock()
	if s.terminal {
		s.mu.Unlock()
		return nil
	}
	bmp := s.Bitmaps[fileIndex]
	if bmp == nil {
		s.mu.Unlock()
		return fmt.Errorf("missing bitmap for file %d", fileIndex)
	}
	if bmp.Has(chunkIndex) {
		s.mu.Unlock()
		// Duplicate delivery is idempotent: acknowledge without counting bytes.
		_ = s.writeControlRaw(protocol.FrameChunkAck, protocol.EncodeIndexPairs([]protocol.IndexPair{{
			FileIndex: df.FileIndex, ChunkIndex: df.ChunkIndex,
		}}))
		return nil
	}
	if s.finalized[fileIndex] {
		s.mu.Unlock()
		return nil
	}
	f, ok := s.partFiles[fileIndex]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("no partial file handle for file %d", fileIndex)
	}

	offset := int64(chunkIndex) * s.ChunkSize
	if _, err := f.WriteAt(df.Data, offset); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("write chunk failed: %w", err)
	}
	if err := f.Sync(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("sync chunk failed: %w", err)
	}

	// Durable ordering: chunk hash -> bitmap -> ACK.
	if s.store != nil {
		if err := s.store.SaveChunkHash(s.TransferID, fileIndex, chunkIndex, computed[:]); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("persist chunk hash failed: %w", err)
		}
	}
	if err := bmp.Set(chunkIndex); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.store != nil {
		if err := s.store.UpdateFileBitmap(s.TransferID, fileIndex, bmp.Bytes(), false); err != nil {
			_ = bmp.Clear(chunkIndex)
			s.mu.Unlock()
			return fmt.Errorf("persist chunk bitmap failed: %w", err)
		}
	}
	if prior, ok := s.priorChunks[fileIndex]; ok {
		delete(prior, chunkIndex)
		if len(prior) == 0 {
			delete(s.priorChunks, fileIndex)
		}
	}
	s.BytesReceived += expectedLen
	delete(s.needAttempts, fmt.Sprintf("%d:%d", fileIndex, chunkIndex))
	completeNow := bmp.IsComplete()
	doneReceived := s.doneReceived
	donePending := s.donePending
	s.mu.Unlock()

	if err := s.writeControlRaw(protocol.FrameChunkAck, protocol.EncodeIndexPairs([]protocol.IndexPair{{
		FileIndex: df.FileIndex, ChunkIndex: df.ChunkIndex,
	}})); err != nil {
		return fmt.Errorf("write CHUNK_ACK failed: %w", err)
	}

	if completeNow {
		s.mu.Lock()
		if !s.finalized[fileIndex] {
			if err := s.finalizeFileLocked(fileIndex); err != nil && !errors.Is(err, errNeedRetry) {
				s.failFileLocked(fileIndex, err)
			}
		}
		s.mu.Unlock()
	}
	if doneReceived {
		return s.complete()
	}
	if donePending {
		s.mu.Lock()
		all := s.allChunksPresentLocked()
		s.mu.Unlock()
		if all {
			return s.complete()
		}
	}
	return nil
}

func (s *ReceiverSession) allChunksPresentLocked() bool {
	for _, e := range s.Entries {
		if e.Type != "file" {
			continue
		}
		bmp := s.Bitmaps[e.Index]
		if bmp == nil || !bmp.IsComplete() {
			return false
		}
	}
	return true
}

func (s *ReceiverSession) complete() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.terminal {
		return nil
	}
	if s.completing {
		return nil
	}
	s.completing = true
	s.doneReceived = true
	defer func() {
		if !s.terminal {
			s.completing = false
		}
	}()

	// Early DONE is a protocol violation unless every chunk has been acked.
	if !s.allChunksPresentLocked() {
		msg := "DONE received before all chunks were received"
		s.sendSessionError(protocol.ErrCodeStateIntegrity, msg, nil, true)
		s.terminal = true
		s.saveStateLocked("Failed", msg)
		return errors.New("cannot complete: missing chunks")
	}

	for _, e := range s.Entries {
		if e.Type != "file" {
			continue
		}
		if s.finalized[e.Index] {
			continue
		}
		if _, failed := s.failedFiles[e.Index]; failed {
			continue
		}
		if err := s.finalizeFileLocked(e.Index); err != nil {
			if errors.Is(err, errNeedRetry) {
				// Verification cleared bits and CHUNK_NEED was sent. The sender
				// has already sent DONE, so completion must be retried after
				// the requested chunks arrive.
				s.doneReceived = false
				s.donePending = true
				s.completing = false
				return nil
			}
			s.failFileLocked(e.Index, err)
		}
	}

	var unfinished []string
	for _, e := range s.Entries {
		if e.Type != "file" {
			continue
		}
		if s.finalized[e.Index] || s.failedFiles[e.Index] != "" {
			continue
		}
		unfinished = append(unfinished, e.Path)
	}
	if len(unfinished) > 0 || len(s.failedFiles) > 0 {
		msg := "not all files were finalized safely"
		if len(s.failedFiles) > 0 {
			parts := make([]string, 0, len(s.failedFiles))
			for idx, reason := range s.failedFiles {
				parts = append(parts, fmt.Sprintf("file %d: %s", idx, reason))
			}
			msg = msg + ": " + strings.Join(parts, "; ")
		}
		if len(unfinished) > 0 {
			msg = msg + "; unfinished: " + strings.Join(unfinished, ", ")
		}
		s.sendSessionError(protocol.ErrCodeStateIntegrity, msg, nil, true)
		s.terminal = true
		s.saveStateLocked("CompletedWithErrors", msg)
		return errors.New(msg)
	}

	doneOk := protocol.DoneOkMessage{
		TransferID:     s.TransferID,
		FilesCompleted: s.FilesCompleted,
		Bytes:          s.BytesReceived,
	}
	s.doneOk = &doneOk
	s.donePending = false
	s.terminal = true
	s.saveStateLocked("Completed", "")

	if err := s.writeControlJSON(protocol.FrameDoneOk, doneOk); err != nil {
		s.saveStateLocked("Failed", "write DONE_OK failed: "+err.Error())
		return err
	}
	// Wake the control read loop so it can observe terminal state.
	_ = s.controlConn.SetReadDeadline(time.Now())
	return nil
}

// finalizeFileLocked requires s.mu to be held.
func (s *ReceiverSession) finalizeFileLocked(fileIndex int) error {
	if s.finalized[fileIndex] {
		return nil
	}
	if fileIndex < 0 || fileIndex >= len(s.Entries) || s.Entries[fileIndex].Type != "file" {
		return fmt.Errorf("invalid file index %d", fileIndex)
	}
	entry := s.Entries[fileIndex]
	bmp := s.Bitmaps[fileIndex]
	if bmp == nil || !bmp.IsComplete() {
		return errNotFinal
	}
	if err := s.verifyPriorChunksLocked(fileIndex); err != nil {
		return err
	}

	finalRel := s.PlannedPaths[fileIndex]
	finalPath := s.finalPaths[fileIndex]
	partPath := s.partPaths[fileIndex]
	mtime := time.UnixMilli(entry.MtimeMs)

	if !s.alreadyFinal[fileIndex] {
		if f, ok := s.partFiles[fileIndex]; ok {
			if err := f.Sync(); err != nil {
				return fmt.Errorf("flush partial file %s failed: %w", finalRel, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close partial file %s failed: %w", finalRel, err)
			}
			delete(s.partFiles, fileIndex)
		}
		if _, err := os.Lstat(partPath); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("stat partial file %s failed: %w", finalRel, err)
			}
			// Zero-byte file: create an empty partial so the commit below is
			// still an atomic no-replace operation.
			empty, createErr := os.OpenFile(partPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
			if createErr != nil {
				return fmt.Errorf("create empty partial %s failed: %w", finalRel, createErr)
			}
			if err := empty.Sync(); err != nil {
				empty.Close()
				return fmt.Errorf("flush empty file %s failed: %w", finalRel, err)
			}
			if err := empty.Close(); err != nil {
				return fmt.Errorf("close empty file %s failed: %w", finalRel, err)
			}
		}
		committed, err := fsutil.CommitNoReplace(partPath, finalPath)
		if err != nil {
			return fmt.Errorf("finalize %s failed: %w", finalRel, err)
		}
		finalPath = committed
		s.finalPaths[fileIndex] = committed
	}

	if err := os.Chtimes(finalPath, mtime, mtime); err != nil {
		return fmt.Errorf("file %s was committed but setting mtime failed: %w", finalRel, err)
	}

	if s.store != nil {
		if err := s.store.UpdateFileBitmap(s.TransferID, fileIndex, bmp.Bytes(), true); err != nil {
			return fmt.Errorf("persist completion for %s failed: %w", finalRel, err)
		}
		relFinal, relErr := filepath.Rel(s.TargetDir, finalPath)
		if relErr != nil {
			return fmt.Errorf("persist final path for %s failed: %w", finalRel, relErr)
		}
		if err := s.store.UpdateFileFinalPath(s.TransferID, fileIndex, filepath.ToSlash(relFinal)); err != nil {
			return fmt.Errorf("persist final path for %s failed: %w", finalRel, err)
		}
	}

	s.finalized[fileIndex] = true
	delete(s.priorChunks, fileIndex)
	s.FilesCompleted++
	if err := s.writeControlJSON(protocol.FrameFileDone, protocol.FileDoneMessage{Index: fileIndex}); err != nil {
		return fmt.Errorf("write FILE_DONE for %s failed: %w", finalRel, err)
	}
	return nil
}

// verifyPriorChunksLocked re-reads chunks written in earlier sessions (or a
// previously finalized file) and compares them with persisted SHA-256 hashes.
// Any mismatch clears the affected bits and requests them again.
func (s *ReceiverSession) verifyPriorChunksLocked(fileIndex int) error {
	prior, ok := s.priorChunks[fileIndex]
	if !ok || len(prior) == 0 {
		return nil
	}
	entry := s.Entries[fileIndex]
	hashes, err := s.loadHashes(fileIndex)
	if err != nil {
		return fmt.Errorf("load chunk hashes failed: %w", err)
	}

	// A file that was already finalized cannot be written in place. If any
	// chunk fails verification, re-receive the whole file into a fresh partial
	// and leave the existing final file untouched.
	if s.alreadyFinal[fileIndex] {
		f, openErr := os.Open(s.finalPaths[fileIndex])
		if openErr != nil {
			return fmt.Errorf("open finalized file for verification failed: %w", openErr)
		}
		bad := false
		for c := range prior {
			length := chunkLen(entry.Size, s.ChunkSize, c)
			buf := make([]byte, length)
			n, readErr := f.ReadAt(buf, int64(c)*s.ChunkSize)
			if readErr != nil && !(errors.Is(readErr, io.EOF) && int64(n) == length) {
				f.Close()
				return fmt.Errorf("re-read chunk %d failed: %w", c, readErr)
			}
			want, ok := hashes[c]
			if int64(n) != length || !ok {
				bad = true
				break
			}
			sum := sha256.Sum256(buf[:n])
			if subtle.ConstantTimeCompare(sum[:], want) != 1 {
				bad = true
				break
			}
		}
		f.Close()
		if !bad {
			delete(s.priorChunks, fileIndex)
			return nil
		}

		chunkCount := chunksForSize(entry.Size, s.ChunkSize)
		bmp := protocol.NewBitmap(chunkCount)
		s.Bitmaps[fileIndex] = bmp
		s.BytesReceived -= entry.Size
		if s.BytesReceived < 0 {
			s.BytesReceived = 0
		}
		s.alreadyFinal[fileIndex] = false
		part, openErr := openPartialFile(s.partPaths[fileIndex])
		if openErr != nil {
			return openErr
		}
		s.partFiles[fileIndex] = part
		delete(s.priorChunks, fileIndex)
		if s.store != nil {
			_ = s.store.UpdateFileBitmap(s.TransferID, fileIndex, bmp.Bytes(), false)
		}
		need := make([]protocol.IndexPair, 0, chunkCount)
		for c := 0; c < chunkCount; c++ {
			need = append(need, protocol.IndexPair{FileIndex: uint32(fileIndex), ChunkIndex: uint32(c)})
		}
		s.sendNeed(need)
		return errNeedRetry
	}

	f, err := os.Open(s.partPaths[fileIndex])
	if err != nil {
		return err
	}
	defer f.Close()

	bmp := s.Bitmaps[fileIndex]
	var need []protocol.IndexPair
	for c := range prior {
		if !bmp.Has(c) {
			delete(prior, c)
			continue
		}
		want, ok := hashes[c]
		if !ok {
			_ = bmp.Clear(c)
			s.BytesReceived -= chunkLen(entry.Size, s.ChunkSize, c)
			need = append(need, protocol.IndexPair{FileIndex: uint32(fileIndex), ChunkIndex: uint32(c)})
			delete(prior, c)
			continue
		}
		length := chunkLen(entry.Size, s.ChunkSize, c)
		buf := make([]byte, length)
		n, readErr := f.ReadAt(buf, int64(c)*s.ChunkSize)
		if readErr != nil && !(errors.Is(readErr, io.EOF) && int64(n) == length) {
			return fmt.Errorf("re-read chunk %d failed: %w", c, readErr)
		}
		if int64(n) != length {
			_ = bmp.Clear(c)
			s.BytesReceived -= length
			need = append(need, protocol.IndexPair{FileIndex: uint32(fileIndex), ChunkIndex: uint32(c)})
			delete(prior, c)
			continue
		}
		sum := sha256.Sum256(buf[:n])
		if subtle.ConstantTimeCompare(sum[:], want) != 1 {
			_ = bmp.Clear(c)
			s.BytesReceived -= length
			need = append(need, protocol.IndexPair{FileIndex: uint32(fileIndex), ChunkIndex: uint32(c)})
			delete(prior, c)
		}
	}

	if len(need) > 0 {
		if s.store != nil {
			_ = s.store.UpdateFileBitmap(s.TransferID, fileIndex, bmp.Bytes(), false)
		}
		if len(prior) == 0 {
			delete(s.priorChunks, fileIndex)
		}
		s.sendNeed(need)
		return errNeedRetry
	}
	delete(s.priorChunks, fileIndex)
	return nil
}

func (s *ReceiverSession) failFileLocked(fileIndex int, err error) {
	if err == nil {
		err = errors.New("unknown finalization failure")
	}
	s.failedFiles[fileIndex] = err.Error()
	idx := fileIndex
	s.sendSessionError(protocol.ErrCodeProtocolError, err.Error(), &idx, false)
	if f, ok := s.partFiles[fileIndex]; ok {
		_ = f.Sync()
		_ = f.Close()
		delete(s.partFiles, fileIndex)
	}
}

func (s *ReceiverSession) saveState(state, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveStateLocked(state, message)
}

func (s *ReceiverSession) saveStateLocked(state, message string) {
	if s.store == nil {
		return
	}
	_ = s.store.SaveTransfer(store.TransferRecord{
		TransferID:      s.TransferID,
		Direction:       "receive",
		PeerFingerprint: s.SenderFP,
		PeerName:        s.SenderName,
		PeerDeviceID:    identity.DeviceIDFromFingerprint(s.SenderFP),
		State:           state,
		ChunkSize:       s.ChunkSize,
		TotalFiles:      s.Offer.FileCount,
		TotalBytes:      s.Offer.TotalBytes,
		CompletedFiles:  s.FilesCompleted,
		CompletedBytes:  s.BytesReceived,
		ManifestHash:    protocol.ComputeManifestHash(s.Entries),
		CreatedAt:       s.createdAt,
		UpdatedAt:       time.Now().UnixMilli(),
		ErrorMessage:    message,
	})
}

func (s *ReceiverSession) noteError(message string) {
	s.mu.Lock()
	terminal := s.terminal
	s.mu.Unlock()
	if terminal {
		return
	}
	s.saveState("Interrupted", message)
}

func (s *ReceiverSession) markTerminal() {
	s.mu.Lock()
	s.terminal = true
	s.mu.Unlock()
}

func (s *ReceiverSession) cancel() {
	s.mu.Lock()
	if s.terminal {
		s.mu.Unlock()
		return
	}
	s.cancelled = true
	s.terminal = true
	s.mu.Unlock()
	s.saveState("Cancelled", "cancelled by peer")
}

func (s *ReceiverSession) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx, f := range s.partFiles {
		_ = f.Sync()
		if err := f.Close(); err != nil {
			slog.Warn("closing partial file failed", "file", idx, "err", err)
		}
		delete(s.partFiles, idx)
	}
	if !s.terminal && !s.cancelled {
		s.saveStateLocked("Interrupted", "connection ended before completion")
	}
}

func newStreamToken() (string, error) {
	var buf [16]byte
	if _, err := io.ReadFull(crand.Reader, buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// cryptoRandRead retained for compatibility with older callers/tests.
func cryptoRandRead(b []byte) {
	if _, err := io.ReadFull(crand.Reader, b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
}
