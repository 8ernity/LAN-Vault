package transfer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"lanvault/core/identity"
	"lanvault/core/protocol"
)

// ProgressCallback is invoked periodically with progress updates
type ProgressCallback func(bytesAcked int64, totalBytes int64, filesDone int, totalFiles int)

// StartSend executes a full file/folder transfer to a receiver
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
) (*protocol.DoneOkMessage, error) {
	// 1. Connect control connection
	clientTLS := senderID.ClientTLSConfig()
	rawControl, err := net.DialTimeout("tcp", receiverAddr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to dial receiver %s: %w", receiverAddr, err)
	}
	defer rawControl.Close()

	controlConn := tls.Client(rawControl, clientTLS)
	if err := controlConn.Handshake(); err != nil {
		return nil, fmt.Errorf("control TLS handshake failed: %w", err)
	}

	// 2. Send HELLO
	hello := protocol.HelloMessage{
		Version:      protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
		DeviceID:     senderID.DeviceID,
		Name:         senderName,
		Platform:     "windows",
		Role:         "control",
		Capabilities: []string{"resume", "multi-stream"},
	}
	if err := protocol.WriteJSONFrame(controlConn, protocol.FrameHello, hello); err != nil {
		return nil, fmt.Errorf("write HELLO failed: %w", err)
	}

	// Read HELLO_OK
	f, err := protocol.ReadFrame(controlConn)
	if err != nil {
		return nil, fmt.Errorf("read HELLO_OK failed: %w", err)
	}
	if f.Type != protocol.FrameHelloOk {
		return nil, fmt.Errorf("expected HELLO_OK, got %s", f.Type)
	}

	// 3. Send OFFER
	dirCount := 0
	for _, it := range items {
		if it.Entry.Type == "dir" {
			dirCount++
		}
	}
	offer := protocol.OfferMessage{
		TransferID:  transferID,
		ChunkSize:   chunkSize,
		FileCount:   totalFiles,
		DirCount:    dirCount,
		TotalBytes:  totalBytes,
		DataStreams: protocol.DataStreamsDefault,
	}
	if err := protocol.WriteJSONFrame(controlConn, protocol.FrameOffer, offer); err != nil {
		return nil, fmt.Errorf("write OFFER failed: %w", err)
	}

	// 4. Batch and send MANIFEST_PART frames
	var manifestEntries []protocol.ManifestEntry
	for _, it := range items {
		manifestEntries = append(manifestEntries, it.Entry)
	}

	parts, err := protocol.BatchManifest(manifestEntries, protocol.ManifestPartMaxEntries, protocol.MaxControlFrame)
	if err != nil {
		return nil, fmt.Errorf("failed to batch manifest: %w", err)
	}

	for _, part := range parts {
		partMsg := protocol.ManifestPartMessage{Entries: part}
		if err := protocol.WriteJSONFrame(controlConn, protocol.FrameManifestPart, partMsg); err != nil {
			return nil, fmt.Errorf("write MANIFEST_PART failed: %w", err)
		}
	}

	// Send MANIFEST_END
	manifestEnd := protocol.ManifestEndMessage{
		EntryCount:   len(manifestEntries),
		ManifestHash: manifestHash,
	}
	if err := protocol.WriteJSONFrame(controlConn, protocol.FrameManifestEnd, manifestEnd); err != nil {
		return nil, fmt.Errorf("write MANIFEST_END failed: %w", err)
	}

	// 5. Read ACCEPT or REJECT
	respFrame, err := protocol.ReadFrame(controlConn)
	if err != nil {
		return nil, fmt.Errorf("read accept/reject failed: %w", err)
	}
	if respFrame.Type == protocol.FrameReject {
		rej, _ := protocol.DecodeJSONPayload[protocol.RejectMessage](respFrame.Payload)
		return nil, fmt.Errorf("transfer rejected: %s (code %d)", rej.Message, rej.Code)
	}
	if respFrame.Type != protocol.FrameAccept {
		return nil, fmt.Errorf("unexpected frame: %s", respFrame.Type)
	}

	accept, err := protocol.DecodeJSONPayload[protocol.AcceptMessage](respFrame.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode ACCEPT failed: %w", err)
	}

	// Decode any existing resume bitmaps
	haveBitmaps := make(map[int]*protocol.Bitmap)
	for fIdxStr, b64 := range accept.Have {
		var fIdx int
		fmt.Sscanf(fIdxStr, "%d", &fIdx)
		if fIdx >= 0 && fIdx < len(items) {
			fileSize := items[fIdx].Entry.Size
			chunks := int((fileSize + chunkSize - 1) / chunkSize)
			bmp, err := protocol.FromBase64(chunks, b64)
			if err == nil {
				haveBitmaps[fIdx] = bmp
			}
		}
	}

	// 6. Open parallel data connections
	dataStreams := accept.DataStreams
	if dataStreams <= 0 {
		dataStreams = 1
	}

	dataConns := make([]net.Conn, dataStreams)
	for i := 0; i < dataStreams; i++ {
		rawD, err := net.DialTimeout("tcp", receiverAddr, 10*time.Second)
		if err != nil {
			return nil, fmt.Errorf("failed to dial data stream %d: %w", i, err)
		}
		tlsD := tls.Client(rawD, clientTLS)
		if err := tlsD.Handshake(); err != nil {
			return nil, fmt.Errorf("data stream %d handshake failed: %w", i, err)
		}
		// Send data HELLO
		dataHello := protocol.HelloMessage{
			Version:     protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
			DeviceID:    senderID.DeviceID,
			Name:        senderName,
			Role:        "data",
			TransferID:  transferID,
			StreamToken: accept.StreamToken,
		}
		if err := protocol.WriteJSONFrame(tlsD, protocol.FrameHello, dataHello); err != nil {
			return nil, fmt.Errorf("data stream %d hello failed: %w", i, err)
		}
		dataConns[i] = tlsD
	}
	defer func() {
		for _, c := range dataConns {
			if c != nil {
				c.Close()
			}
		}
	}()

	// 7. Stream DATA frames and receive CHUNK_ACKs
	var totalAckedBytes int64 = 0
	var totalFilesDone int32 = 0
	ackChan := make(chan protocol.IndexPair, 1000)
	errChan := make(chan error, 2)
	doneOkChan := make(chan *protocol.DoneOkMessage, 1)

	// Start control connection reader (listening for CHUNK_ACK, FILE_DONE, CHUNK_NEED, DONE_OK, ERROR)
	go func() {
		for {
			cf, err := protocol.ReadFrame(controlConn)
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
					errChan <- err
				}
				return
			}
			switch cf.Type {
			case protocol.FrameChunkAck:
				pairs, err := protocol.DecodeIndexPairs(cf.Payload)
				if err == nil {
					for _, p := range pairs {
						ackChan <- p
					}
				}
			case protocol.FrameFileDone:
				atomic.AddInt32(&totalFilesDone, 1)
			case protocol.FrameDoneOk:
				doneOk, err := protocol.DecodeJSONPayload[protocol.DoneOkMessage](cf.Payload)
				if err == nil {
					doneOkChan <- doneOk
				}
				return
			case protocol.FrameError:
				errChan <- fmt.Errorf("receiver error frame: %s", string(cf.Payload))
				return
			}
		}
	}()

	// Track window per stream
	streamIdx := 0
	var streamMu sync.Mutex

	for _, item := range items {
		if item.Entry.Type != "file" || item.Entry.Size == 0 {
			continue
		}

		f, err := os.Open(item.SourceDiskPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open source file %s: %w", item.SourceDiskPath, err)
		}

		numChunks := int((item.Entry.Size + chunkSize - 1) / chunkSize)
		bmp := haveBitmaps[item.Entry.Index]

		for chunkIdx := 0; chunkIdx < numChunks; chunkIdx++ {
			// Skip if receiver already has chunk
			if bmp != nil && bmp.IsSet(chunkIdx) {
				continue
			}

			offset := int64(chunkIdx) * chunkSize
			currChunkLen := chunkSize
			if offset+currChunkLen > item.Entry.Size {
				currChunkLen = item.Entry.Size - offset
			}

			buf := make([]byte, currChunkLen)
			if _, err := f.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
				f.Close()
				return nil, fmt.Errorf("failed to read chunk %d of %s: %w", chunkIdx, item.SourceDiskPath, err)
			}

			dataPayload := protocol.EncodeDataFrame(uint32(item.Entry.Index), uint32(chunkIdx), buf)

			// Round-robin send to parallel data streams
			streamMu.Lock()
			targetConn := dataConns[streamIdx%dataStreams]
			streamIdx++
			streamMu.Unlock()

			if err := protocol.WriteRawFrame(targetConn, protocol.FrameData, dataPayload); err != nil {
				f.Close()
				return nil, fmt.Errorf("failed to write data frame: %w", err)
			}

			// Read ACKs if available to track progress
			for {
				select {
				case <-ackChan:
					atomic.AddInt64(&totalAckedBytes, currChunkLen)
					if progressCb != nil {
						progressCb(totalAckedBytes, totalBytes, int(totalFilesDone), totalFiles)
					}
				case err := <-errChan:
					f.Close()
					return nil, err
				default:
					goto NextChunk
				}
			}
		NextChunk:
		}
		f.Close()
	}

	// 8. Send DONE frame on control connection
	if err := protocol.WriteJSONFrame(controlConn, protocol.FrameDone, protocol.DoneMessage{TransferID: transferID}); err != nil {
		return nil, fmt.Errorf("write DONE failed: %w", err)
	}

	// 9. Read DONE_OK via channel
	select {
	case doneOk := <-doneOkChan:
		return doneOk, nil
	case err := <-errChan:
		return nil, err
	case <-time.After(30 * time.Second):
		return nil, errors.New("timeout waiting for DONE_OK")
	}
}
