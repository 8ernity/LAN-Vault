package transfer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"lanvault/core/identity"
	"lanvault/core/protocol"
)

// ProgressCallback is invoked periodically with progress updates.
type ProgressCallback func(bytesAcked int64, totalBytes int64, filesDone int, totalFiles int)

var errProtocol = errors.New("protocol violation")

// StartSend executes a full file/folder transfer to a receiver.
//
// If expectedPeerFP is supplied, the TLS handshake is pinned to that
// fingerprint. An empty value means the caller is performing a first-contact
// unpaired send; the receiver still decides whether to allow it.
func StartSend(
	receiverAddr string,
	senderID *identity.Identity,
	senderName string,
	transferID string,
	items []ScannedItem,
	totalFiles int,
	totalBytes int64,
	manifestHash string,
	chunkSize int64,
	progressCb ProgressCallback,
	expectedPeerFP ...string,
) (*protocol.DoneOkMessage, error) {
	if senderID == nil {
		return nil, errors.New("sender identity is required")
	}
	if transferID == "" || len(transferID) > 128 {
		return nil, errors.New("invalid transfer_id")
	}
	if chunkSize < protocol.ChunkSizeMin || chunkSize > protocol.ChunkSizeMax {
		return nil, fmt.Errorf("chunk size %d out of bounds", chunkSize)
	}

	var manifestEntries []protocol.ManifestEntry
	var fileCount, dirCount int
	var sumBytes int64
	for _, it := range items {
		manifestEntries = append(manifestEntries, it.Entry)
		switch it.Entry.Type {
		case "file":
			if it.Entry.Size < 0 {
				return nil, fmt.Errorf("negative size for %s", it.Entry.Path)
			}
			fileCount++
			sumBytes += it.Entry.Size
		case "dir":
			if it.Entry.Size != 0 {
				return nil, fmt.Errorf("directory %s has non-zero size", it.Entry.Path)
			}
			dirCount++
		default:
			return nil, fmt.Errorf("invalid manifest entry type %q", it.Entry.Type)
		}
	}
	if fileCount != totalFiles || sumBytes != totalBytes {
		return nil, fmt.Errorf("manifest summary mismatch: files %d/%d bytes %d/%d", fileCount, totalFiles, sumBytes, totalBytes)
	}
	if err := protocol.ValidateManifestIndices(manifestEntries); err != nil {
		return nil, err
	}
	computedHash := protocol.ComputeManifestHash(manifestEntries)
	if !strings.EqualFold(computedHash, manifestHash) {
		return nil, fmt.Errorf("manifest hash mismatch: caller supplied %s, computed %s", manifestHash, computedHash)
	}

	var clientTLS *tls.Config
	expectedFP := ""
	if len(expectedPeerFP) > 0 {
		expectedFP = expectedPeerFP[0]
	}
	if expectedFP != "" {
		clientTLS = senderID.ClientTLSConfigPinned(expectedFP)
	} else {
		clientTLS = senderID.ClientTLSConfig()
	}

	rawControl, err := net.DialTimeout("tcp", receiverAddr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to dial receiver %s: %w", receiverAddr, err)
	}
	defer rawControl.Close()

	controlConn := tls.Client(rawControl, clientTLS)
	_ = controlConn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := controlConn.Handshake(); err != nil {
		return nil, fmt.Errorf("control TLS handshake failed: %w", err)
	}
	_ = controlConn.SetDeadline(time.Time{})

	peerCerts := controlConn.ConnectionState().PeerCertificates
	if len(peerCerts) == 0 {
		return nil, errors.New("receiver presented no certificate")
	}
	peerFP := identity.FingerprintFromDER(peerCerts[0].Raw)
	if expectedFP != "" && !strings.EqualFold(peerFP, expectedFP) {
		return nil, fmt.Errorf("receiver fingerprint %s does not match expected %s", peerFP, expectedFP)
	}
	dataTLS := senderID.ClientTLSConfigPinned(peerFP)

	// HELLO
	hello := protocol.HelloMessage{
		Version:      protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
		DeviceID:     senderID.DeviceID,
		Name:         senderName,
		Platform:     "windows",
		Role:         "control",
		Capabilities: []string{"resume", "multi-stream"},
	}
	if err := writeControlJSON(controlConn, protocol.FrameHello, hello); err != nil {
		return nil, fmt.Errorf("write HELLO failed: %w", err)
	}

	_ = controlConn.SetReadDeadline(time.Now().Add(time.Duration(protocol.HelloTimeoutMs) * time.Millisecond))
	f, err := protocol.ReadFrame(controlConn)
	if err != nil {
		return nil, fmt.Errorf("read HELLO_OK failed: %w", err)
	}
	_ = controlConn.SetReadDeadline(time.Time{})
	if f.Type == protocol.FrameError {
		msg, _ := protocol.DecodeJSONPayload[protocol.ErrorMessage](f.Payload)
		if msg != nil {
			return nil, fmt.Errorf("receiver rejected HELLO: %s", msg.Message)
		}
		return nil, errors.New("receiver rejected HELLO")
	}
	if f.Type != protocol.FrameHelloOk {
		return nil, fmt.Errorf("expected HELLO_OK, got %s", f.Type)
	}
	helloOk, err := protocol.DecodeJSONPayload[protocol.HelloOkMessage](f.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode HELLO_OK failed: %w", err)
	}
	if helloOk.Version.Major != protocol.ProtocolMajor {
		return nil, fmt.Errorf("unsupported receiver protocol major %d", helloOk.Version.Major)
	}
	if !strings.EqualFold(helloOk.DeviceID, identity.DeviceIDFromFingerprint(peerFP)) {
		return nil, fmt.Errorf("receiver device_id does not match its certificate fingerprint")
	}

	// OFFER + manifest
	offer := protocol.OfferMessage{
		TransferID:  transferID,
		ChunkSize:   chunkSize,
		FileCount:   fileCount,
		DirCount:    dirCount,
		TotalBytes:  totalBytes,
		DataStreams: protocol.DataStreamsDefault,
	}
	if err := writeControlJSON(controlConn, protocol.FrameOffer, offer); err != nil {
		return nil, fmt.Errorf("write OFFER failed: %w", err)
	}
	parts, err := protocol.BatchManifest(manifestEntries, protocol.ManifestPartMaxEntries, protocol.MaxControlFrame)
	if err != nil {
		return nil, fmt.Errorf("failed to batch manifest: %w", err)
	}
	for _, part := range parts {
		if err := writeControlJSON(controlConn, protocol.FrameManifestPart, protocol.ManifestPartMessage{Entries: part}); err != nil {
			return nil, fmt.Errorf("write MANIFEST_PART failed: %w", err)
		}
	}
	if err := writeControlJSON(controlConn, protocol.FrameManifestEnd, protocol.ManifestEndMessage{
		EntryCount:   len(manifestEntries),
		ManifestHash: computedHash,
	}); err != nil {
		return nil, fmt.Errorf("write MANIFEST_END failed: %w", err)
	}

	_ = controlConn.SetReadDeadline(time.Now().Add(10 * time.Minute))
	respFrame, err := protocol.ReadFrame(controlConn)
	if err != nil {
		return nil, fmt.Errorf("read ACCEPT/REJECT failed: %w", err)
	}
	_ = controlConn.SetReadDeadline(time.Time{})
	if respFrame.Type == protocol.FrameReject {
		rej, _ := protocol.DecodeJSONPayload[protocol.RejectMessage](respFrame.Payload)
		if rej != nil {
			return nil, fmt.Errorf("transfer rejected: %s (code %d)", rej.Message, rej.Code)
		}
		return nil, errors.New("transfer rejected")
	}
	if respFrame.Type == protocol.FrameError {
		msg, _ := protocol.DecodeJSONPayload[protocol.ErrorMessage](respFrame.Payload)
		if msg != nil {
			return nil, fmt.Errorf("receiver error: %s", msg.Message)
		}
		return nil, errors.New("receiver error")
	}
	if respFrame.Type != protocol.FrameAccept {
		return nil, fmt.Errorf("unexpected frame: %s", respFrame.Type)
	}
	accept, err := protocol.DecodeJSONPayload[protocol.AcceptMessage](respFrame.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode ACCEPT failed: %w", err)
	}
	if accept.TransferID != transferID {
		return nil, fmt.Errorf("ACCEPT transfer_id mismatch")
	}
	if accept.StreamToken == "" {
		return nil, fmt.Errorf("ACCEPT missing stream_token")
	}
	dataStreams := accept.DataStreams
	if dataStreams < 1 || dataStreams > offer.DataStreams || dataStreams > protocol.DataStreamsMax {
		return nil, fmt.Errorf("ACCEPT data_streams %d is invalid for offered %d", dataStreams, offer.DataStreams)
	}

	// Validate `have` before using it. A lying receiver can only lose its own
	// data, but malformed state must still fail closed.
	itemsMap := make(map[int]ScannedItem, len(items))
	chunkCounts := make(map[int]int, len(items))
	for _, it := range items {
		itemsMap[it.Entry.Index] = it
		if it.Entry.Type == "file" {
			chunkCounts[it.Entry.Index] = chunksForSize(it.Entry.Size, chunkSize)
		}
	}
	haveBitmaps := make(map[int]*protocol.Bitmap)
	for fIdxStr, b64 := range accept.Have {
		fIdx, convErr := strconv.Atoi(fIdxStr)
		if convErr != nil || fIdx < 0 {
			_ = writeControlJSON(controlConn, protocol.FrameError, protocol.ErrorMessage{
				Code: int(protocol.ErrCodeProtocolError), Message: "invalid file index in resume bitmap", Fatal: true,
			})
			return nil, fmt.Errorf("invalid have key %q", fIdxStr)
		}
		it, ok := itemsMap[fIdx]
		if !ok || it.Entry.Type != "file" {
			_ = writeControlJSON(controlConn, protocol.FrameError, protocol.ErrorMessage{
				Code: int(protocol.ErrCodeProtocolError), Message: "resume bitmap names an unknown file", Fatal: true,
			})
			return nil, fmt.Errorf("have names unknown file %d", fIdx)
		}
		bmp, bmpErr := protocol.FromBase64(chunkCounts[fIdx], b64)
		if bmpErr != nil {
			_ = writeControlJSON(controlConn, protocol.FrameError, protocol.ErrorMessage{
				Code: int(protocol.ErrCodeProtocolError), Message: "invalid resume bitmap: " + bmpErr.Error(), Fatal: true,
			})
			return nil, fmt.Errorf("invalid resume bitmap for file %d: %w", fIdx, bmpErr)
		}
		haveBitmaps[fIdx] = bmp
	}

	// Open data connections and validate the receiver's HELLO_OK on each.
	dataConns := make([]net.Conn, dataStreams)
	defer func() {
		for _, c := range dataConns {
			if c != nil {
				_ = c.Close()
			}
		}
	}()
	for i := 0; i < dataStreams; i++ {
		rawD, err := net.DialTimeout("tcp", receiverAddr, 10*time.Second)
		if err != nil {
			return nil, fmt.Errorf("failed to dial data stream %d: %w", i, err)
		}
		tlsD := tls.Client(rawD, dataTLS)
		_ = tlsD.SetDeadline(time.Now().Add(10 * time.Second))
		if err := tlsD.Handshake(); err != nil {
			rawD.Close()
			return nil, fmt.Errorf("data stream %d handshake failed: %w", i, err)
		}
		if err := writeControlJSON(tlsD, protocol.FrameHello, protocol.HelloMessage{
			Version:     protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
			DeviceID:    senderID.DeviceID,
			Name:        senderName,
			Platform:    "windows",
			Role:        "data",
			TransferID:  transferID,
			StreamToken: accept.StreamToken,
		}); err != nil {
			tlsD.Close()
			return nil, fmt.Errorf("data stream %d HELLO failed: %w", i, err)
		}
		df, err := protocol.ReadFrame(tlsD)
		if err != nil {
			tlsD.Close()
			return nil, fmt.Errorf("data stream %d HELLO_OK failed: %w", i, err)
		}
		if df.Type != protocol.FrameHelloOk {
			tlsD.Close()
			if df.Type == protocol.FrameError {
				msg, _ := protocol.DecodeJSONPayload[protocol.ErrorMessage](df.Payload)
				if msg != nil {
					return nil, fmt.Errorf("receiver rejected data stream %d: %s", i, msg.Message)
				}
			}
			return nil, fmt.Errorf("data stream %d expected HELLO_OK, got %s", i, df.Type)
		}
		_ = tlsD.SetDeadline(time.Time{})
		dataConns[i] = tlsD
	}

	// Control reader.
	var filesDone int32
	var controlWriteMu sync.Mutex
	writeControlLocked := func(ft protocol.FrameType, v any) error {
		controlWriteMu.Lock()
		defer controlWriteMu.Unlock()
		_ = controlConn.SetWriteDeadline(time.Now().Add(30 * time.Second))
		return protocol.WriteJSONFrame(controlConn, ft, v)
	}

	ackChan := make(chan protocol.IndexPair, 4096)
	needChan := make(chan protocol.IndexPair, 1024)
	errChan := make(chan error, 8)
	doneOkChan := make(chan *protocol.DoneOkMessage, 1)

	reportErr := func(err error) {
		select {
		case errChan <- err:
		default:
		}
	}

	go func() {
		for {
			cf, err := protocol.ReadFrame(controlConn)
			if err != nil {
				reportErr(fmt.Errorf("control connection closed: %w", err))
				return
			}
			switch cf.Type {
			case protocol.FrameChunkAck:
				pairs, err := protocol.DecodeIndexPairs(cf.Payload)
				if err != nil {
					reportErr(fmt.Errorf("%w: malformed CHUNK_ACK", errProtocol))
					return
				}
				for _, p := range pairs {
					select {
					case ackChan <- p:
					default:
						reportErr(fmt.Errorf("%w: CHUNK_ACK flood", errProtocol))
						return
					}
				}
			case protocol.FrameChunkNeed:
				pairs, err := protocol.DecodeIndexPairs(cf.Payload)
				if err != nil {
					reportErr(fmt.Errorf("%w: malformed CHUNK_NEED", errProtocol))
					return
				}
				if len(pairs) > protocol.MaxNeedPairsPerFrame {
					reportErr(fmt.Errorf("%w: CHUNK_NEED exceeds pair limit", errProtocol))
					return
				}
				for _, p := range pairs {
					select {
					case needChan <- p:
					default:
						reportErr(fmt.Errorf("%w: CHUNK_NEED flood", errProtocol))
						return
					}
				}
			case protocol.FrameFileDone:
				msg, err := protocol.DecodeJSONPayload[protocol.FileDoneMessage](cf.Payload)
				if err != nil {
					reportErr(fmt.Errorf("%w: malformed FILE_DONE", errProtocol))
					return
				}
				if it, ok := itemsMap[msg.Index]; !ok || it.Entry.Type != "file" {
					reportErr(fmt.Errorf("%w: FILE_DONE names unknown file %d", errProtocol, msg.Index))
					return
				}
				atomic.AddInt32(&filesDone, 1)
			case protocol.FrameDoneOk:
				doneOk, err := protocol.DecodeJSONPayload[protocol.DoneOkMessage](cf.Payload)
				if err != nil {
					reportErr(fmt.Errorf("%w: malformed DONE_OK", errProtocol))
					return
				}
				doneOkChan <- doneOk
				return
			case protocol.FrameError:
				msg, _ := protocol.DecodeJSONPayload[protocol.ErrorMessage](cf.Payload)
				if msg != nil {
					reportErr(fmt.Errorf("receiver error (code %d): %s", msg.Code, msg.Message))
				} else {
					reportErr(errors.New("receiver error"))
				}
				return
			case protocol.FramePing:
				controlWriteMu.Lock()
				_ = protocol.WriteRawFrame(controlConn, protocol.FramePong, nil)
				controlWriteMu.Unlock()
			case protocol.FramePong:
				// keepalive
			default:
				reportErr(fmt.Errorf("%w: unexpected control frame %s", errProtocol, cf.Type))
				return
			}
		}
	}()

	acked := make(map[string]bool)
	sent := make(map[string]bool)
	inFlight := make(map[string]bool)
	streamOf := make(map[string]int)
	streamInFlight := make([]int, dataStreams)
	needOutstanding := make(map[string]int)
	maxNeedOutstanding := 4 * protocol.WindowChunksPerStream * dataStreams
	var ackedBytes int64
	nextStream := 0

	keyOf := func(fileIndex, chunkIndex int) string {
		return strconv.Itoa(fileIndex) + ":" + strconv.Itoa(chunkIndex)
	}
	validatePair := func(p protocol.IndexPair, requireSent bool) (ScannedItem, int, error) {
		fIdx := int(p.FileIndex)
		cIdx := int(p.ChunkIndex)
		it, ok := itemsMap[fIdx]
		if !ok || it.Entry.Type != "file" {
			return ScannedItem{}, 0, fmt.Errorf("%w: unknown file_index %d", errProtocol, fIdx)
		}
		count := chunkCounts[fIdx]
		if cIdx < 0 || cIdx >= count {
			return ScannedItem{}, 0, fmt.Errorf("%w: chunk_index %d out of range for file %d", errProtocol, cIdx, fIdx)
		}
		if requireSent && !sent[keyOf(fIdx, cIdx)] {
			return ScannedItem{}, 0, fmt.Errorf("%w: ACK/NEED for chunk that was never sent", errProtocol)
		}
		return it, cIdx, nil
	}

	processAck := func(p protocol.IndexPair) error {
		it, cIdx, err := validatePair(p, true)
		if err != nil {
			return err
		}
		fIdx := it.Entry.Index
		k := keyOf(fIdx, cIdx)
		if acked[k] {
			return fmt.Errorf("%w: duplicate CHUNK_ACK for file %d chunk %d", errProtocol, fIdx, cIdx)
		}
		if !inFlight[k] {
			return fmt.Errorf("%w: CHUNK_ACK for chunk that is not in flight", errProtocol)
		}
		delete(inFlight, k)
		acked[k] = true
		if s, ok := streamOf[k]; ok {
			if streamInFlight[s] > 0 {
				streamInFlight[s]--
			}
			delete(streamOf, k)
		}
		length := chunkLen(it.Entry.Size, chunkSize, cIdx)
		atomic.AddInt64(&ackedBytes, length)
		if progressCb != nil {
			progressCb(atomic.LoadInt64(&ackedBytes), totalBytes, int(atomic.LoadInt32(&filesDone)), totalFiles)
		}
		return nil
	}

	openSource := func(it ScannedItem) (*os.File, error) {
		f, err := os.Open(it.SourceDiskPath)
		if err != nil {
			return nil, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		if info.Size() != it.Entry.Size {
			f.Close()
			return nil, fmt.Errorf("source file size changed for %s", it.Entry.Path)
		}
		if it.Entry.MtimeMs != 0 && info.ModTime().UnixMilli() != it.Entry.MtimeMs {
			f.Close()
			return nil, fmt.Errorf("source file mtime changed for %s", it.Entry.Path)
		}
		return f, nil
	}

	readChunk := func(f *os.File, it ScannedItem, cIdx int) ([]byte, error) {
		length := chunkLen(it.Entry.Size, chunkSize, cIdx)
		buf := make([]byte, length)
		n, err := f.ReadAt(buf, int64(cIdx)*chunkSize)
		if err != nil && !(errors.Is(err, io.EOF) && int64(n) == length) {
			return nil, err
		}
		if int64(n) != length {
			return nil, fmt.Errorf("short read for %s chunk %d", it.Entry.Path, cIdx)
		}
		return buf, nil
	}

	var pump func() error
	var retransmit func(protocol.IndexPair) error
	waitForEvent := func() error {
		if err := pump(); err != nil {
			return err
		}
		select {
		case p := <-ackChan:
			return processAck(p)
		case n := <-needChan:
			return retransmit(n)
		case err := <-errChan:
			return err
		case <-time.After(30 * time.Second):
			return errors.New("timeout waiting for ACK window")
		}
	}
	streamForNext := func() (int, error) {
		for {
			for i := 0; i < dataStreams; i++ {
				candidate := (nextStream + i) % dataStreams
				if streamInFlight[candidate] < protocol.WindowChunksPerStream {
					nextStream = (candidate + 1) % dataStreams
					return candidate, nil
				}
			}
			if err := waitForEvent(); err != nil {
				return 0, err
			}
		}
	}

	writeData := func(stream int, it ScannedItem, cIdx int, data []byte) error {
		payload := protocol.EncodeDataFrame(uint32(it.Entry.Index), uint32(cIdx), data)
		_ = dataConns[stream].SetWriteDeadline(time.Now().Add(30 * time.Second))
		if err := protocol.WriteRawFrame(dataConns[stream], protocol.FrameData, payload); err != nil {
			return fmt.Errorf("write DATA failed: %w", err)
		}
		k := keyOf(it.Entry.Index, cIdx)
		if !inFlight[k] {
			inFlight[k] = true
			streamOf[k] = stream
			streamInFlight[stream]++
		}
		sent[k] = true
		return nil
	}

	retransmit = func(p protocol.IndexPair) error {
		it, cIdx, err := validatePair(p, false)
		if err != nil {
			return err
		}
		fIdx := it.Entry.Index
		k := keyOf(fIdx, cIdx)
		if acked[k] {
			return fmt.Errorf("%w: CHUNK_NEED for an already acked chunk", errProtocol)
		}
		needOutstanding[k]++
		if needOutstanding[k] > maxNeedOutstanding {
			return fmt.Errorf("%w: too many outstanding re-requests for file %d chunk %d", errProtocol, fIdx, cIdx)
		}
		f, err := openSource(it)
		if err != nil {
			return fmt.Errorf("source changed during transfer (file %d): %w", fIdx, err)
		}
		defer f.Close()
		data, err := readChunk(f, it, cIdx)
		if err != nil {
			return fmt.Errorf("re-read for retransmit failed: %w", err)
		}

		var stream int
		if inFlight[k] {
			stream = streamOf[k]
		} else {
			stream, err = streamForNext()
			if err != nil {
				return err
			}
		}
		return writeData(stream, it, cIdx, data)
	}

	pump = func() error {
		for {
			select {
			case p := <-ackChan:
				if err := processAck(p); err != nil {
					return err
				}
			case n := <-needChan:
				if err := retransmit(n); err != nil {
					return err
				}
			case err := <-errChan:
				return err
			default:
				return nil
			}
		}
	}

	// Send all missing chunks.
	for _, item := range items {
		if item.Entry.Type != "file" || item.Entry.Size == 0 {
			continue
		}
		f, err := openSource(item)
		if err != nil {
			idx := item.Entry.Index
			_ = writeControlLocked(protocol.FrameError, protocol.ErrorMessage{
				Code: int(protocol.ErrCodeFileChanged), Message: err.Error(), FileIndex: &idx, Fatal: true,
			})
			return nil, fmt.Errorf("source file changed before sending: %w", err)
		}

		numChunks := chunkCounts[item.Entry.Index]
		bmp := haveBitmaps[item.Entry.Index]
		for cIdx := 0; cIdx < numChunks; cIdx++ {
			if bmp != nil && bmp.Has(cIdx) {
				continue
			}
			k := keyOf(item.Entry.Index, cIdx)
			if acked[k] {
				continue
			}
			if err := pump(); err != nil {
				f.Close()
				return nil, err
			}
			stream, err := streamForNext()
			if err != nil {
				f.Close()
				return nil, err
			}
			data, err := readChunk(f, item, cIdx)
			if err != nil {
				idx := item.Entry.Index
				_ = writeControlLocked(protocol.FrameError, protocol.ErrorMessage{
					Code: int(protocol.ErrCodeFileChanged), Message: err.Error(), FileIndex: &idx, Fatal: true,
				})
				f.Close()
				return nil, fmt.Errorf("read chunk failed (source changed?): %w", err)
			}
			if err := writeData(stream, item, cIdx, data); err != nil {
				f.Close()
				return nil, err
			}
		}
		f.Close()
	}

	// Completion barrier: all sent chunks must be acked, including any
	// re-requests issued by the receiver.
	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	for {
		if len(inFlight) > 0 {
			select {
			case p := <-ackChan:
				if err := processAck(p); err != nil {
					return nil, err
				}
			case n := <-needChan:
				if err := retransmit(n); err != nil {
					return nil, err
				}
			case err := <-errChan:
				return nil, err
			case <-deadline.C:
				return nil, errors.New("timeout waiting for remaining chunk ACKs")
			}
			continue
		}
		// Nothing in flight. Give the receiver a moment to send any final
		// CHUNK_NEED caused by resume verification, but never declare success
		// for a chunk that was never acked.
		select {
		case p := <-ackChan:
			if err := processAck(p); err != nil {
				return nil, err
			}
			continue
		case n := <-needChan:
			if err := retransmit(n); err != nil {
				return nil, err
			}
			continue
		case err := <-errChan:
			return nil, err
		default:
		}
		break
	}

	// Every chunk that was not covered by `have` must have a valid ACK.
	for _, item := range items {
		if item.Entry.Type != "file" || item.Entry.Size == 0 {
			continue
		}
		bmp := haveBitmaps[item.Entry.Index]
		for c := 0; c < chunkCounts[item.Entry.Index]; c++ {
			if bmp != nil && bmp.Has(c) {
				continue
			}
			if !acked[keyOf(item.Entry.Index, c)] {
				return nil, fmt.Errorf("%w: file %d chunk %d was never acknowledged", errProtocol, item.Entry.Index, c)
			}
		}
	}

	if err := writeControlLocked(protocol.FrameDone, protocol.DoneMessage{TransferID: transferID}); err != nil {
		return nil, fmt.Errorf("write DONE failed: %w", err)
	}

	doneTimer := time.NewTimer(5 * time.Minute)
	defer doneTimer.Stop()
	for {
		select {
		case doneOk := <-doneOkChan:
			if doneOk.TransferID != transferID {
				return nil, fmt.Errorf("DONE_OK transfer_id mismatch")
			}
			if doneOk.FilesCompleted < 0 || doneOk.FilesCompleted > totalFiles {
				return nil, fmt.Errorf("DONE_OK files_completed %d out of range", doneOk.FilesCompleted)
			}
			if doneOk.Bytes < 0 || doneOk.Bytes > totalBytes {
				return nil, fmt.Errorf("DONE_OK bytes %d out of range", doneOk.Bytes)
			}
			return doneOk, nil
		case p := <-ackChan:
			if err := processAck(p); err != nil {
				return nil, err
			}
		case n := <-needChan:
			if err := retransmit(n); err != nil {
				return nil, err
			}
		case err := <-errChan:
			return nil, err
		case <-doneTimer.C:
			return nil, errors.New("timeout waiting for DONE_OK")
		}
	}
}

// writeControlJSON is a small helper used before the control reader starts.
func writeControlJSON(conn net.Conn, ft protocol.FrameType, v any) error {
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	defer conn.SetWriteDeadline(time.Time{})
	return protocol.WriteJSONFrame(conn, ft, v)
}
