package main

import (
	"context"
	"crypto/tls"
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
				req := IncomingRequest{
					TransferID: offer.TransferID,
					SenderName: peerName,
					SenderFP:   peerFP,
					FileCount:  offer.FileCount,
					TotalBytes: offer.TotalBytes,
					ShortFP:    identity.ShortFingerprintGrouped(peerFP),
				}
				runtime.EventsEmit(a.ctx, "transfer:incoming", req)

				ch := make(chan bool, 1)
				a.mu.Lock()
				a.pendingDecisions[offer.TransferID] = ch
				a.mu.Unlock()

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

// StartSend initiates file transfer to target
func (a *App) StartSend(targetAddr string, paths []string) error {
	items, totalFiles, totalBytes, manifestHash, err := transfer.ScanSourceItems(paths)
	if err != nil {
		return err
	}

	transferID := fmt.Sprintf("tr-%d", time.Now().UnixNano())
	go func() {
		progressCb := func(bytesAcked int64, total int64, filesDone int, totalFiles int) {
			runtime.EventsEmit(a.ctx, "transfer:progress", map[string]any{
				"bytesAcked": bytesAcked,
				"totalBytes": total,
				"filesDone":  filesDone,
				"totalFiles": totalFiles,
			})
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
		)
		if err != nil {
			runtime.EventsEmit(a.ctx, "transfer:error", err.Error())
		} else {
			runtime.EventsEmit(a.ctx, "transfer:completed", doneOk)
		}
	}()

	return nil
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
