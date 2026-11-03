package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"lanvault/core/diag"
	"lanvault/core/discovery"
	"lanvault/core/identity"
	"lanvault/core/netutil"
	"lanvault/core/pairing"
	"lanvault/core/protocol"
	"lanvault/core/store"
	"lanvault/core/transfer"
)

// AppState provides runtime overview for the UI
type AppState struct {
	DeviceID         string   `json:"device_id"`
	Fingerprint      string   `json:"fingerprint"`
	ShortFingerprint string   `json:"short_fingerprint"`
	DeviceName       string   `json:"device_name"`
	LocalIPs         []string `json:"local_ips"`
	Port             int      `json:"port"`
	SaveDir          string   `json:"save_dir"`
	Visibility       string   `json:"visibility"`
}

// ScannedSelection holds metadata after scanning selected items
type ScannedSelection struct {
	Items        []transfer.ScannedItem `json:"items"`
	TotalFiles   int                    `json:"total_files"`
	TotalBytes   int64                  `json:"total_bytes"`
	ManifestHash string                 `json:"manifest_hash"`
}

// IncomingRequest holds data for modal consent prompt
type IncomingRequest struct {
	TransferID      string `json:"transfer_id"`
	SenderName      string `json:"sender_name"`
	SenderFP        string `json:"sender_fp"`
	FileCount       int    `json:"file_count"`
	TotalBytes      int64  `json:"total_bytes"`
	ShortFP         string `json:"short_fp"`
}

// App manages the backend Wails bridge
type App struct {
	ctx             context.Context
	id              *identity.Identity
	st              *store.Store
	tracker         *discovery.Tracker
	broadcaster     *discovery.BeaconBroadcaster
	beaconListener  *discovery.BeaconListener
	tcpListener     net.Listener
	pairingMgr      *pairing.Manager
	dataDir         string
	saveDir         string
	deviceName      string
	visibility      string
	mu              sync.Mutex
	pendingDecisions map[string]chan bool
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		tracker:          discovery.NewTracker(),
		pairingMgr:       pairing.NewManager(),
		visibility:       "Everyone nearby",
		pendingDecisions: make(map[string]chan bool),
	}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	transfer.SetPairingManager(a.pairingMgr)

	configDir, _ := os.UserConfigDir()
	a.dataDir = filepath.Join(configDir, "LANVault")
	os.MkdirAll(a.dataDir, 0700)

	downloads, _ := os.UserHomeDir()
	a.saveDir = filepath.Join(downloads, "Downloads", "LANVault")
	os.MkdirAll(a.saveDir, 0755)

	// Load or generate identity
	id, err := identity.LoadPEM(a.dataDir)
	if err != nil {
		id, err = identity.GenerateNewIdentity()
		if err == nil {
			id.SavePEM(a.dataDir)
		}
	}
	a.id = id

	hostname, _ := os.Hostname()
	a.deviceName = hostname

	// Open store
	st, err := store.OpenStore(filepath.Join(a.dataDir, "lanvault.db"))
	if err == nil {
		a.st = st
		if name := st.GetSetting("general.device_name", ""); name != "" {
			a.deviceName = name
		} else if name := st.GetSetting("device_name", ""); name != "" {
			a.deviceName = name
		}
		if sDir := st.GetSetting("receive.save_location", ""); sDir != "" {
			a.saveDir = sDir
		} else if sDir := st.GetSetting("save_directory", ""); sDir != "" {
			a.saveDir = sDir
		}
		if vis := st.GetSetting("visibility", ""); vis != "" {
			a.visibility = vis
		}
	}

	// Start TCP listener
	serverTLS := a.id.ServerTLSConfig()
	l, err := tls.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", protocol.DefaultPort), serverTLS)
	if err == nil {
		a.tcpListener = l
		go a.acceptLoop()
	}

	// Start UDP discovery
	a.broadcaster = discovery.NewBeaconBroadcaster(a.id.DeviceID, a.deviceName, "windows", protocol.DefaultPort)
	a.broadcaster.Start()

	bl, err := discovery.StartBeaconListener(a.tracker, protocol.BeaconPort, a.id.DeviceID)
	if err == nil {
		a.beaconListener = bl
	}

	// Subscribe to peer changes and emit to frontend
	a.tracker.Subscribe(func(peers []discovery.DiscoveredPeer) {
		runtime.EventsEmit(a.ctx, "devices:updated", peers)
	})

	// Periodic peer cleanup ticker
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.tracker.ExpireStalePeers(6 * time.Second)
		}
	}()
}

func (a *App) acceptLoop() {
	for {
		conn, err := a.tcpListener.Accept()
		if err != nil {
			return
		}

		go func(c net.Conn) {
			defer c.Close()
			onDecision := func(offer protocol.OfferMessage, peerName, peerFP string) (bool, string) {
				ch := make(chan bool, 1)
				a.mu.Lock()
				a.pendingDecisions[offer.TransferID] = ch
				a.mu.Unlock()

				req := IncomingRequest{
					TransferID: offer.TransferID,
					SenderName: peerName,
					SenderFP:   peerFP,
					FileCount:  offer.FileCount,
					TotalBytes: offer.TotalBytes,
					ShortFP:    identity.ShortFingerprintGrouped(peerFP),
				}
				runtime.EventsEmit(a.ctx, "transfer:incoming", req)

				select {
				case accept := <-ch:
					return accept, a.saveDir
				case <-time.After(time.Duration(protocol.OfferResponseTimeoutSec) * time.Second):
					return false, ""
				}
			}

			if err := transfer.HandleIncomingControl(c, a.id, a.st, onDecision, a.tcpListener); err != nil {
				runtime.EventsEmit(a.ctx, "transfer:error", err.Error())
			} else {
				runtime.EventsEmit(a.ctx, "transfer:completed", "Transfer completed successfully")
			}
		}(conn)
	}
}

// GetAppState returns current application status
func (a *App) GetAppState() AppState {
	ips := netutil.GetLocalIPStrings()
	return AppState{
		DeviceID:         a.id.DeviceID,
		Fingerprint:      a.id.Fingerprint,
		ShortFingerprint: identity.ShortFingerprintGrouped(a.id.Fingerprint),
		DeviceName:       a.deviceName,
		LocalIPs:         ips,
		Port:             protocol.DefaultPort,
		SaveDir:          a.saveDir,
		Visibility:       a.visibility,
	}
}

// GetSettings loads settings from SQLite
func (a *App) GetSettings() map[string]string {
	if a.st == nil {
		return map[string]string{}
	}
	s, _ := a.st.GetAllSettings()
	return s
}

// UpdateSetting updates a single setting
func (a *App) UpdateSetting(key, val string) error {
	if a.st == nil {
		return nil
	}
	if key == "general.device_name" || key == "device_name" {
		a.deviceName = val
		_ = a.st.SetSetting("general.device_name", val)
		return a.st.SetSetting("device_name", val)
	} else if key == "receive.save_location" || key == "save_directory" || key == "save_dir" {
		a.saveDir = val
		_ = a.st.SetSetting("receive.save_location", val)
		return a.st.SetSetting("save_directory", val)
	} else if key == "visibility" {
		a.visibility = val
		return a.st.SetSetting("visibility", val)
	}
	return a.st.SetSetting(key, val)
}

// ListNearbyDevices returns list of discovered devices
func (a *App) ListNearbyDevices() []discovery.DiscoveredPeer {
	return a.tracker.ListPeers()
}

// Rescan clears and restarts peer discovery
func (a *App) Rescan() {
	if a.broadcaster != nil {
		a.broadcaster.Stop()
		a.broadcaster = discovery.NewBeaconBroadcaster(a.id.DeviceID, a.deviceName, "windows", protocol.DefaultPort)
		a.broadcaster.Start()
	}
}

// ListTrustedDevices returns paired devices
func (a *App) ListTrustedDevices() []store.TrustedDevice {
	if a.st == nil {
		return []store.TrustedDevice{}
	}
	devs, _ := a.st.ListTrustedDevices()
	return devs
}

// RemoveDevice removes trusted peer
func (a *App) RemoveDevice(fp string) error {
	if a.st == nil {
		return nil
	}
	return a.st.DeleteTrustedDevice(fp)
}

// GetPairingQR creates fresh QR pairing URI
func (a *App) GetPairingQR() (string, error) {
	tok, err := a.pairingMgr.CreateNewToken()
	if err != nil {
		return "", err
	}
	ips := netutil.GetLocalIPStrings()
	return pairing.GenerateURI(protocol.ProtocolMajor, a.id.Fingerprint, ips, protocol.DefaultPort, a.deviceName, tok), nil
}

// PickFiles opens file dialog
func (a *App) PickFiles() ([]string, error) {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose Files to Send",
	})
	return paths, err
}

// PickFolder opens directory picker
func (a *App) PickFolder() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose Folder to Send",
	})
	return dir, err
}

// ScanItems scans paths for transfer review
func (a *App) ScanItems(paths []string) (*ScannedSelection, error) {
	items, totalFiles, totalBytes, manifestHash, err := transfer.ScanSourceItems(paths)
	if err != nil {
		return nil, err
	}
	return &ScannedSelection{
		Items:        items,
		TotalFiles:   totalFiles,
		TotalBytes:   totalBytes,
		ManifestHash: manifestHash,
	}, nil
}

// StartSend initiates file transfer to target. expectedPeerFP pins the TLS
// certificate of the selected peer; an empty value is only valid for the
// explicit manual-address path, and the receiver still enforces pairing.
func (a *App) StartSend(targetAddr string, paths []string, expectedPeerFP string) error {
	items, totalFiles, totalBytes, manifestHash, err := transfer.ScanSourceItems(paths)
	if err != nil {
		return err
	}

	// Stable transfer IDs make retries resumable while still creating a new
	// transfer after a previous attempt reached a terminal state.
	seed := sha256.Sum256([]byte(manifestHash + "|" + expectedPeerFP + "|" + targetAddr))
	baseID := "tr-" + hex.EncodeToString(seed[:16])
	transferID := baseID
	var createdAt = time.Now().UnixMilli()
	if a.st != nil {
		if existing, getErr := a.st.GetTransfer(baseID); getErr == nil && existing != nil {
			switch existing.State {
			case "Completed", "CompletedWithErrors", "Cancelled", "Failed":
				transferID = fmt.Sprintf("%s-%d", baseID, time.Now().UnixNano())
			default:
				transferID = baseID
				createdAt = existing.CreatedAt
			}
		} else if getErr != nil && !errors.Is(getErr, store.ErrNotFound) {
			return getErr
		}
	}

	if a.st != nil {
		if err := a.st.SaveTransfer(store.TransferRecord{
			TransferID:      transferID,
			Direction:       "send",
			PeerFingerprint: expectedPeerFP,
			PeerName:        targetAddr,
			State:           "Transferring",
			ChunkSize:       protocol.ChunkSizeDefault,
			TotalFiles:      totalFiles,
			TotalBytes:      totalBytes,
			ManifestHash:    manifestHash,
			CreatedAt:       createdAt,
			UpdatedAt:       time.Now().UnixMilli(),
		}); err != nil {
			return fmt.Errorf("record transfer start: %w", err)
		}
	}

	go func() {
		progressCb := func(bytesAcked int64, total int64, filesDone int, totalFiles int) {
			runtime.EventsEmit(a.ctx, "transfer:progress", map[string]any{
				"transferId": transferID,
				"bytesAcked": bytesAcked,
				"totalBytes": total,
				"filesDone":  filesDone,
				"totalFiles": totalFiles,
			})
			if a.st != nil {
				_ = a.st.SaveTransfer(store.TransferRecord{
					TransferID:     transferID,
					State:          "Transferring",
					CompletedFiles: filesDone,
					CompletedBytes: bytesAcked,
					UpdatedAt:      time.Now().UnixMilli(),
				})
			}
		}

		peerArgs := []string{}
		if expectedPeerFP != "" {
			peerArgs = append(peerArgs, expectedPeerFP)
		}
		doneOk, err := transfer.StartSend(
			targetAddr,
			a.id,
			a.deviceName,
			transferID,
			items,
			totalFiles,
			totalBytes,
			manifestHash,
			protocol.ChunkSizeDefault,
			progressCb,
			peerArgs...,
		)
		if err != nil {
			if a.st != nil {
				_ = a.st.SaveTransfer(store.TransferRecord{
					TransferID:   transferID,
					State:        "Interrupted",
					ErrorMessage: err.Error(),
					UpdatedAt:    time.Now().UnixMilli(),
				})
			}
			runtime.EventsEmit(a.ctx, "transfer:error", map[string]any{
				"transferId": transferID,
				"error":      err.Error(),
			})
			return
		}
		if a.st != nil {
			_ = a.st.SaveTransfer(store.TransferRecord{
				TransferID:     transferID,
				State:          "Completed",
				CompletedFiles: doneOk.FilesCompleted,
				CompletedBytes: doneOk.Bytes,
				UpdatedAt:      time.Now().UnixMilli(),
			})
		}
		runtime.EventsEmit(a.ctx, "transfer:completed", map[string]any{
			"transferId":     transferID,
			"filesCompleted": doneOk.FilesCompleted,
			"bytes":          doneOk.Bytes,
		})
	}()

	return nil
}

// PairWithURI initiates a pairing handshake with a remote device specified by QR URI
func (a *App) PairWithURI(rawURI string) error {
	params, err := pairing.ParseURI(rawURI)
	if err != nil {
		return fmt.Errorf("invalid pairing URI: %w", err)
	}

	var conn net.Conn
	var dialErr error
	for _, ip := range params.IPs {
		addr := net.JoinHostPort(ip, fmt.Sprintf("%d", params.Port))
		clientTLS := a.id.ClientTLSConfigPinned(params.Fingerprint)
		rawConn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			dialErr = err
			continue
		}
		tlsConn := tls.Client(rawConn, clientTLS)
		if err := tlsConn.Handshake(); err != nil {
			rawConn.Close()
			dialErr = err
			continue
		}
		conn = tlsConn
		break
	}
	if conn == nil {
		return fmt.Errorf("failed to connect to pairing target: %w", dialErr)
	}
	defer conn.Close()

	// 1. Send HELLO
	hello := protocol.HelloMessage{
		Version:      protocol.Version{Major: protocol.ProtocolMajor, Minor: protocol.ProtocolMinor},
		DeviceID:     a.id.DeviceID,
		Name:         a.deviceName,
		Platform:     "windows",
		Role:         "control",
		Capabilities: []string{"resume", "multi-stream"},
	}
	if err := protocol.WriteJSONFrame(conn, protocol.FrameHello, hello); err != nil {
		return fmt.Errorf("write HELLO failed: %w", err)
	}

	// 2. Read HELLO_OK
	hOkFrame, err := protocol.ReadFrame(conn)
	if err != nil {
		return fmt.Errorf("read HELLO_OK failed: %w", err)
	}
	if hOkFrame.Type != protocol.FrameHelloOk {
		return fmt.Errorf("expected HELLO_OK, got %s", hOkFrame.Type)
	}

	// 3. Send PAIR_REQUEST
	pairReq := protocol.PairRequestMessage{
		Token:    params.RawToken,
		Name:     a.deviceName,
		Platform: "windows",
	}
	if err := protocol.WriteJSONFrame(conn, protocol.FramePairRequest, pairReq); err != nil {
		return fmt.Errorf("write PAIR_REQUEST failed: %w", err)
	}

	// 4. Read PAIR_OK or ERROR
	respFrame, err := protocol.ReadFrame(conn)
	if err != nil {
		return fmt.Errorf("read PAIR_OK failed: %w", err)
	}
	if respFrame.Type == protocol.FramePairOk {
		pairOk, err := protocol.DecodeJSONPayload[protocol.PairOkMessage](respFrame.Payload)
		if err != nil {
			return err
		}
		if a.st != nil {
			_ = a.st.SaveTrustedDevice(store.TrustedDevice{
				Fingerprint: params.Fingerprint,
				DeviceID:    pairOk.DeviceID,
				Name:        pairOk.Name,
				Platform:    "remote",
				PairedAt:    time.Now().UnixMilli(),
				LastSeen:    time.Now().UnixMilli(),
			})
		}
		return nil
	} else if respFrame.Type == protocol.FrameError {
		errMsg, _ := protocol.DecodeJSONPayload[protocol.ErrorMessage](respFrame.Payload)
		return fmt.Errorf("pairing rejected: %s", errMsg.Message)
	}

	return fmt.Errorf("unexpected frame response: %s", respFrame.Type)
}

// RespondIncomingTransfer answers pending offer
func (a *App) RespondIncomingTransfer(transferID string, accept bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ch, ok := a.pendingDecisions[transferID]; ok {
		ch <- accept
		delete(a.pendingDecisions, transferID)
	}
}

// ListRecentTransfers loads history
func (a *App) ListRecentTransfers(limit int) []store.TransferRecord {
	if a.st == nil {
		return []store.TransferRecord{}
	}
	recs, _ := a.st.ListRecentTransfers(limit)
	return recs
}

// RunDiagnostics executes system diagnostic checks
func (a *App) RunDiagnostics() []diag.DiagnosticResult {
	return diag.RunDiagnostics()
}
