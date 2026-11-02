package transfer

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"lanvault/core/fsutil"
	"lanvault/core/identity"
	"lanvault/core/protocol"
	"lanvault/core/store"
)

// DecisionCallback is called when an OFFER arrives to decide whether to accept
type DecisionCallback func(offer protocol.OfferMessage, peerName, peerFP string) (accept bool, targetDir string)

// ReceiverSession manages a single incoming transfer on the receiving end
type ReceiverSession struct {
	TransferID      string
	SenderFP        string
	SenderName      string
	Offer           protocol.OfferMessage
	Entries         []protocol.ManifestEntry
	PlannedPaths    []string
	TargetDir       string
	ChunkSize       int64
	StreamToken     string
	DataStreams     int
	Bitmaps         map[int]*protocol.Bitmap // file_index -> Bitmap
	FilesCompleted  int
	BytesReceived   int64

	store           *store.Store
	controlConn     net.Conn
	dataConns       []net.Conn
	mu              sync.Mutex
	partFiles       map[int]*os.File // open .lvpart file handles
	ackQueue        []protocol.IndexPair
	ackTimer        *time.Timer
	doneChan        chan struct{}
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

func unregisterActiveSession(transferID string) {
	activeSessionsMu.Lock()
	defer activeSessionsMu.Unlock()
	delete(activeSessions, transferID)
}

func getActiveSession(transferID string) *ReceiverSession {
	activeSessionsMu.Lock()
	defer activeSessionsMu.Unlock()
	return activeSessions[transferID]
}

// StartReceiverControl starts handling an incoming control or data connection from a sender
func HandleIncomingControl(
	controlConn net.Conn,
	serverID *identity.Identity,
	st *store.Store,
	onDecision DecisionCallback,
	dataListener net.Listener,
) error {
	tlsConn, ok := controlConn.(*tls.Conn)
	if !ok {
		return errors.New("control connection must be TLS")
	}

	if err := tlsConn.Handshake(); err != nil {
		return fmt.Errorf("server control handshake failed: %w", err)
	}

	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return errors.New("missing client certificate")
	}
	peerFP := identity.FingerprintFromDER(state.PeerCertificates[0].Raw)

	// 1. Read HELLO
	frame, err := protocol.ReadFrame(tlsConn)
	if err != nil {
		return fmt.Errorf("read HELLO failed: %w", err)
	}
	if frame.Type != protocol.FrameHello {
		return fmt.Errorf("expected HELLO, got %s", frame.Type)
	}

	hello, err := protocol.DecodeJSONPayload[protocol.HelloMessage](frame.Payload)
	if err != nil {
		return err
	}
	if hello.Version.Major != protocol.ProtocolMajor {
		protocol.WriteJSONFrame(tlsConn, protocol.FrameError, protocol.ErrorMessage{
			Code:    int(protocol.ErrCodeUnsupportedVersion),
			Message: "Unsupported wire protocol major version",
			Fatal:   true,
		})
		return errors.New("unsupported protocol major version")
	}

	// Reply HELLO_OK
	helloOk := protocol.HelloOkMessage{
		Version:      protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
		DeviceID:     serverID.DeviceID,
		Name:         "LANVault Receiver",
		Platform:     "windows",
		Capabilities: []string{"resume", "multi-stream"},
		Paired:       true, // In v1.0 manual acceptance applies to all
	}

	// If this connection is a parallel DATA stream, route it directly to the active session
	if hello.Role == "data" {
		sess := getActiveSession(hello.TransferID)
		if sess == nil || sess.StreamToken != hello.StreamToken {
			protocol.WriteJSONFrame(tlsConn, protocol.FrameError, protocol.ErrorMessage{
				Code:    int(protocol.ErrCodeProtocolError),
				Message: "Invalid data connection credentials",
				Fatal:   true,
			})
			return errors.New("invalid data connection credentials")
		}

		if err := protocol.WriteJSONFrame(tlsConn, protocol.FrameHelloOk, helloOk); err != nil {
			return fmt.Errorf("write data HELLO_OK failed: %w", err)
		}

		// Read DATA frames until connection closes or transfer finishes
		for {
			df, err := protocol.ReadFrame(tlsConn)
			if err != nil {
				return nil
			}
			if df.Type == protocol.FrameData {
				if err := sess.handleDataFrame(df.Payload); err != nil {
					slog.Error("failed to handle data frame", "err", err)
				}
			} else if df.Type == protocol.FrameDone {
				sess.flushAndFinalizeAll()
				doneOk := protocol.DoneOkMessage{
					TransferID:     sess.TransferID,
					FilesCompleted: sess.FilesCompleted,
					Bytes:          sess.BytesReceived,
				}
				_ = protocol.WriteJSONFrame(sess.controlConn, protocol.FrameDoneOk, doneOk)
				_ = protocol.WriteJSONFrame(tlsConn, protocol.FrameDoneOk, doneOk)
				st.SaveTransfer(store.TransferRecord{
					TransferID:     sess.TransferID,
					State:          "Completed",
					CompletedFiles: sess.FilesCompleted,
					CompletedBytes: sess.BytesReceived,
					UpdatedAt:      time.Now().UnixMilli(),
				})
				return nil
			} else if df.Type == protocol.FramePing {
				protocol.WriteRawFrame(tlsConn, protocol.FramePong, nil)
			}
		}
	}

	if err := protocol.WriteJSONFrame(tlsConn, protocol.FrameHelloOk, helloOk); err != nil {
		return fmt.Errorf("write HELLO_OK failed: %w", err)
	}

	// 2. Read OFFER
	frame, err = protocol.ReadFrame(tlsConn)
	if err != nil {
		return fmt.Errorf("read OFFER failed: %w", err)
	}
	if frame.Type != protocol.FrameOffer {
		return fmt.Errorf("expected OFFER, got %s", frame.Type)
	}

	offer, err := protocol.DecodeJSONPayload[protocol.OfferMessage](frame.Payload)
	if err != nil {
		return err
	}

	// 3. Read MANIFEST_PART frames until MANIFEST_END
	var entries []protocol.ManifestEntry
	var manifestEnd *protocol.ManifestEndMessage

	for {
		f, err := protocol.ReadFrame(tlsConn)
		if err != nil {
			return fmt.Errorf("read manifest frame failed: %w", err)
		}
		if f.Type == protocol.FrameManifestPart {
			part, err := protocol.DecodeJSONPayload[protocol.ManifestPartMessage](f.Payload)
			if err != nil {
				return err
			}
			entries = append(entries, part.Entries...)
		} else if f.Type == protocol.FrameManifestEnd {
			end, err := protocol.DecodeJSONPayload[protocol.ManifestEndMessage](f.Payload)
			if err != nil {
				return err
			}
			manifestEnd = end
			break
		} else {
			return fmt.Errorf("unexpected frame during manifest: %s", f.Type)
		}
	}

	// Validate manifest
	if err := protocol.ValidateManifestIndices(entries); err != nil {
		protocol.WriteJSONFrame(tlsConn, protocol.FrameError, protocol.ErrorMessage{
			Code:    int(protocol.ErrCodeProtocolError),
			Message: err.Error(),
			Fatal:   true,
		})
		return err
	}

	calcHash := protocol.ComputeManifestHash(entries)
	if calcHash != manifestEnd.ManifestHash {
		protocol.WriteJSONFrame(tlsConn, protocol.FrameError, protocol.ErrorMessage{
			Code:    int(protocol.ErrCodeHashMismatch),
			Message: "Manifest hash mismatch",
			Fatal:   true,
		})
		return errors.New("manifest hash mismatch")
	}

	// 4. Validate Path Security and collisions via fsutil
	inputs := make([]fsutil.EntryInput, len(entries))
	for i, e := range entries {
		inputs[i] = fsutil.EntryInput{Type: e.Type, Path: e.Path}
	}
	plannedPaths, rejectReason, err := fsutil.PlanManifest(inputs, "windows")
	if err != nil || rejectReason != "" {
		protocol.WriteJSONFrame(tlsConn, protocol.FrameReject, protocol.RejectMessage{
			TransferID: offer.TransferID,
			Code:       int(protocol.ErrCodePathInvalid),
			Message:    fmt.Sprintf("Invalid path or namespace conflict: %s", rejectReason),
		})
		return fmt.Errorf("manifest rejected: %s", rejectReason)
	}

	// 5. Check user consent / decision
	accept, targetDir := onDecision(*offer, hello.Name, peerFP)
	if !accept {
		protocol.WriteJSONFrame(tlsConn, protocol.FrameReject, protocol.RejectMessage{
			TransferID: offer.TransferID,
			Code:       int(protocol.ErrCodeRejected),
			Message:    "Declined by user",
		})
		return nil
	}

	// Check if this transfer is a resume from store
	haveBitmaps := make(map[string]string)
	isResume := false
	existingTransfer, err := st.GetTransfer(offer.TransferID)
	if err == nil && existingTransfer != nil && existingTransfer.ManifestHash == calcHash && existingTransfer.PeerFingerprint == peerFP {
		isResume = true
		files, _ := st.GetTransferFiles(offer.TransferID)
		for _, f := range files {
			if len(f.ChunkBitmap) > 0 {
				bmp, err := protocol.FromBytes(f.ChunkCount, f.ChunkBitmap)
				if err == nil && bmp.CompletedCount() > 0 {
					haveBitmaps[fmt.Sprintf("%d", f.FileIndex)] = bmp.ToBase64()
				}
			}
		}
	}

	// Generate stream token for data connections
	var tokenBuf [16]byte
	cryptoRandRead(tokenBuf[:])
	streamToken := base64.RawURLEncoding.EncodeToString(tokenBuf[:])

	dataStreams := offer.DataStreams
	if dataStreams <= 0 || dataStreams > protocol.DataStreamsMax {
		dataStreams = protocol.DataStreamsDefault
	}

	// 6. Save or update transfer in store
	st.SaveTransfer(store.TransferRecord{
		TransferID:      offer.TransferID,
		Direction:       "receive",
		PeerFingerprint: peerFP,
		PeerName:        hello.Name,
		PeerDeviceID:    hello.DeviceID,
		State:           "Transferring",
		ChunkSize:       offer.ChunkSize,
		TotalFiles:      offer.FileCount,
		TotalBytes:      offer.TotalBytes,
		ManifestHash:    calcHash,
		CreatedAt:       time.Now().UnixMilli(),
		UpdatedAt:       time.Now().UnixMilli(),
	})

	// Pre-create directories and initialize files
	var fileRecords []store.TransferFileRecord
	bitmaps := make(map[int]*protocol.Bitmap)
	partFiles := make(map[int]*os.File)

	for i, e := range entries {
		finalRelPath := plannedPaths[i]
		fullDestPath := filepath.Join(targetDir, filepath.FromSlash(finalRelPath))

		chunkCount := 0
		if e.Type == "dir" {
			if err := os.MkdirAll(fullDestPath, 0755); err != nil {
				slog.Error("failed to create directory", "path", fullDestPath, "err", err)
			}
		} else {
			if e.Size > 0 {
				chunkCount = int((e.Size + offer.ChunkSize - 1) / offer.ChunkSize)
			}
			bmp := protocol.NewBitmap(chunkCount)
			bitmaps[e.Index] = bmp

			// Create parent directory if needed
			os.MkdirAll(filepath.Dir(fullDestPath), 0755)

			// Open or create .lvpart file
			partPath := fullDestPath + ".lvpart"
			f, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0644)
			if err != nil {
				return fmt.Errorf("failed to open part file %s: %w", partPath, err)
			}
			partFiles[e.Index] = f

			fileRecords = append(fileRecords, store.TransferFileRecord{
				TransferID:  offer.TransferID,
				FileIndex:   e.Index,
				Type:        e.Type,
				WirePath:    e.Path,
				FinalPath:   finalRelPath,
				Size:        e.Size,
				MtimeMs:     e.MtimeMs,
				ChunkCount:  chunkCount,
				ChunkBitmap: bmp.Bytes(),
				IsCompleted: false,
			})
		}
	}
	st.SaveTransferFiles(fileRecords)

	// Send ACCEPT
	acceptMsg := protocol.AcceptMessage{
		TransferID:  offer.TransferID,
		Resume:      isResume,
		StreamToken: streamToken,
		DataStreams: dataStreams,
		Have:        haveBitmaps,
	}
	if err := protocol.WriteJSONFrame(tlsConn, protocol.FrameAccept, acceptMsg); err != nil {
		return fmt.Errorf("write ACCEPT failed: %w", err)
	}

	// 7. Accept Data Connections and start chunk reception
	sess := &ReceiverSession{
		TransferID:     offer.TransferID,
		SenderFP:       peerFP,
		SenderName:     hello.Name,
		Offer:          *offer,
		Entries:        entries,
		PlannedPaths:   plannedPaths,
		TargetDir:      targetDir,
		ChunkSize:      offer.ChunkSize,
		StreamToken:    streamToken,
		DataStreams:    dataStreams,
		Bitmaps:        bitmaps,
		FilesCompleted: 0,
		store:          st,
		controlConn:    tlsConn,
		partFiles:      partFiles,
		doneChan:       make(chan struct{}),
	}
	defer sess.cleanup()

	registerActiveSession(sess)
	defer unregisterActiveSession(sess.TransferID)

	// If a dedicated dataListener was passed (e.g. in unit tests), accept data connections from it
	if dataListener != nil {
		for i := 0; i < dataStreams; i++ {
			go func() {
				conn, err := dataListener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_ = HandleIncomingControl(conn, serverID, st, onDecision, nil)
			}()
		}
	}

	// Read control messages (DONE, CANCEL, PAUSE)
	for {
		ctrlFrame, err := protocol.ReadFrame(tlsConn)
		if err != nil {
			// If already completed via data connection, treat EOF as normal close
			if rec, errRec := st.GetTransfer(sess.TransferID); errRec == nil && rec != nil && rec.State == "Completed" {
				return nil
			}
			return err
		}

		if ctrlFrame.Type == protocol.FrameDone {
			// Sender says all chunks sent
			sess.flushAndFinalizeAll()
			doneOk := protocol.DoneOkMessage{
				TransferID:     sess.TransferID,
				FilesCompleted: sess.FilesCompleted,
				Bytes:          sess.BytesReceived,
			}
			protocol.WriteJSONFrame(tlsConn, protocol.FrameDoneOk, doneOk)
			st.SaveTransfer(store.TransferRecord{
				TransferID:     sess.TransferID,
				State:          "Completed",
				CompletedFiles: sess.FilesCompleted,
				CompletedBytes: sess.BytesReceived,
				UpdatedAt:      time.Now().UnixMilli(),
			})
			return nil
		} else if ctrlFrame.Type == protocol.FrameCancel {
			st.SaveTransfer(store.TransferRecord{
				TransferID: sess.TransferID,
				State:      "Cancelled",
				UpdatedAt:  time.Now().UnixMilli(),
			})
			return nil
		}
	}
}

func (s *ReceiverSession) handleDataFrame(payload []byte) error {
	df, err := protocol.DecodeDataFramePayload(payload)
	if err != nil {
		return err
	}

	// Verify SHA-256 hash of chunk
	computedHash := sha256.Sum256(df.Data)
	if computedHash != df.Hash {
		// Mismatch: queue CHUNK_NEED
		s.mu.Lock()
		need := protocol.IndexPair{FileIndex: df.FileIndex, ChunkIndex: df.ChunkIndex}
		protocol.WriteRawFrame(s.controlConn, protocol.FrameChunkNeed, protocol.EncodeIndexPairs([]protocol.IndexPair{need}))
		s.mu.Unlock()
		return fmt.Errorf("chunk hash mismatch for file %d chunk %d", df.FileIndex, df.ChunkIndex)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	fileHandle, ok := s.partFiles[int(df.FileIndex)]
	if !ok {
		return fmt.Errorf("no file handle for file index %d", df.FileIndex)
	}

	// Write chunk to partial file at offset
	offset := int64(df.ChunkIndex) * s.ChunkSize
	if _, err := fileHandle.WriteAt(df.Data, offset); err != nil {
		return fmt.Errorf("writeAt failed: %w", err)
	}

	// Update bitmap
	bmp, ok := s.Bitmaps[int(df.FileIndex)]
	if ok {
		bmp.Set(int(df.ChunkIndex))
	}
	s.BytesReceived += int64(len(df.Data))

	// Queue ACK
	ackPair := protocol.IndexPair{FileIndex: df.FileIndex, ChunkIndex: df.ChunkIndex}
	protocol.WriteRawFrame(s.controlConn, protocol.FrameChunkAck, protocol.EncodeIndexPairs([]protocol.IndexPair{ackPair}))

	// If file completed, finalize it
	if bmp != nil && bmp.IsComplete() {
		s.finalizeFile(int(df.FileIndex))
	}

	return nil
}

func (s *ReceiverSession) finalizeFile(fileIndex int) error {
	fileHandle, ok := s.partFiles[fileIndex]
	if ok {
		fileHandle.Sync()
		fileHandle.Close()
		delete(s.partFiles, fileIndex)
	}

	finalRelPath := s.PlannedPaths[fileIndex]
	fullDestPath := filepath.Join(s.TargetDir, filepath.FromSlash(finalRelPath))
	partPath := fullDestPath + ".lvpart"

	// Atomic rename to final path
	if err := os.Rename(partPath, fullDestPath); err != nil {
		return fmt.Errorf("rename to final path failed: %w", err)
	}

	// Set mtime
	entry := s.Entries[fileIndex]
	mtime := time.UnixMilli(entry.MtimeMs)
	os.Chtimes(fullDestPath, mtime, mtime)

	s.FilesCompleted++

	// Send FILE_DONE
	protocol.WriteJSONFrame(s.controlConn, protocol.FrameFileDone, protocol.FileDoneMessage{Index: fileIndex})

	// Update store
	bmp := s.Bitmaps[fileIndex]
	if bmp != nil {
		s.store.UpdateFileBitmap(s.TransferID, fileIndex, bmp.Bytes(), true)
	}
	return nil
}

func (s *ReceiverSession) flushAndFinalizeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for idx := range s.partFiles {
		s.finalizeFile(idx)
	}
}

func (s *ReceiverSession) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, f := range s.partFiles {
		f.Sync()
		f.Close()
	}
}

func cryptoRandRead(b []byte) {
	for i := range b {
		b[i] = byte(time.Now().UnixNano() + int64(i))
	}
}
