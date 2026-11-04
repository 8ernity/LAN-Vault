package discovery

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"lanvault/core/netutil"
	"lanvault/core/protocol"
)

// DiscoveredPeer models a nearby device discovered via mDNS or UDP beacon
type DiscoveredPeer struct {
	DeviceID   string   `json:"device_id"`
	Name       string   `json:"name"`
	Platform   string   `json:"platform"`
	IPs        []string `json:"ips"`
	Port       int      `json:"port"`
	LastSeen   time.Time `json:"last_seen"`
	Source     string   `json:"source"` // "mdns", "beacon", or "manual"
}

// BeaconPayload is the JSON datagram broadcast over UDP BEACON_PORT
type BeaconPayload struct {
	V        int    `json:"v"`
	ID       string `json:"id"`
	DeviceID string `json:"device_id,omitempty"`
	Name     string `json:"name"`
	Plat     string `json:"plat"`
	Platform string `json:"platform,omitempty"`
	Port     int    `json:"port"`
}

// Tracker tracks discovered peers, deduplicating by device_id and expiring stale peers
type Tracker struct {
	mu        sync.RWMutex
	peers     map[string]*DiscoveredPeer // device_id -> peer
	listeners []func(peers []DiscoveredPeer)
}

// NewTracker creates a new peer tracker
func NewTracker() *Tracker {
	return &Tracker{
		peers: make(map[string]*DiscoveredPeer),
	}
}

// UpdatePeer adds or updates a discovered peer
func (t *Tracker) UpdatePeer(peer DiscoveredPeer) {
	t.mu.Lock()
	defer t.mu.Unlock()

	existing, ok := t.peers[peer.DeviceID]
	if ok {
		existing.Name = peer.Name
		existing.Platform = peer.Platform
		existing.Port = peer.Port
		existing.LastSeen = time.Now()
		// Merge IPs without duplicates
		ipMap := make(map[string]bool)
		for _, ip := range existing.IPs {
			ipMap[ip] = true
		}
		for _, ip := range peer.IPs {
			if !ipMap[ip] {
				existing.IPs = append(existing.IPs, ip)
				ipMap[ip] = true
			}
		}
	} else {
		peer.LastSeen = time.Now()
		t.peers[peer.DeviceID] = &peer
	}
	t.notifyListeners()
}

// ExpireStalePeers removes peers not seen within timeout (default 3 beacon intervals = 6s)
func (t *Tracker) ExpireStalePeers(timeout time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	changed := false
	for id, p := range t.peers {
		if now.Sub(p.LastSeen) > timeout {
			delete(t.peers, id)
			changed = true
		}
	}
	if changed {
		t.notifyListeners()
	}
}

// ListPeers returns current list of discovered peers
func (t *Tracker) ListPeers() []DiscoveredPeer {
	t.mu.RLock()
	defer t.mu.RUnlock()

	res := make([]DiscoveredPeer, 0, len(t.peers))
	for _, p := range t.peers {
		res = append(res, *p)
	}
	return res
}

func (t *Tracker) notifyListeners() {
	var list []DiscoveredPeer
	for _, p := range t.peers {
		list = append(list, *p)
	}
	for _, fn := range t.listeners {
		fn(list)
	}
}

// Subscribe registers a callback invoked when the peer list updates
func (t *Tracker) Subscribe(fn func([]DiscoveredPeer)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.listeners = append(t.listeners, fn)
}

// BeaconBroadcaster broadcasts UDP beacons every BEACON_INTERVAL_MS
type BeaconBroadcaster struct {
	mu         sync.RWMutex
	deviceID   string
	name       string
	platform   string
	port       int
	visibility string
	stopChan   chan struct{}
}

// NewBeaconBroadcaster creates a broadcaster
func NewBeaconBroadcaster(deviceID, name, platform string, port int) *BeaconBroadcaster {
	return &BeaconBroadcaster{
		deviceID:   deviceID,
		name:       name,
		platform:   platform,
		port:       port,
		visibility: "everyone",
		stopChan:   make(chan struct{}),
	}
}

// SetVisibility updates the advertising visibility mode ("everyone", "contacts", "hidden")
func (b *BeaconBroadcaster) SetVisibility(v string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.visibility = v
}

// SetName updates the advertised device name
func (b *BeaconBroadcaster) SetName(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.name = name
}

// Start begins broadcasting on BEACON_PORT (47801) every BEACON_INTERVAL_MS (2000 ms)
func (b *BeaconBroadcaster) Start() {
	ticker := time.NewTicker(time.Duration(protocol.BeaconIntervalMs) * time.Millisecond)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-b.stopChan:
				return
			case <-ticker.C:
				b.broadcastOnce()
			}
		}
	}()
}

// Stop stops the beacon broadcaster
func (b *BeaconBroadcaster) Stop() {
	select {
	case <-b.stopChan:
	default:
		close(b.stopChan)
	}
}

func (b *BeaconBroadcaster) broadcastOnce() {
	b.mu.RLock()
	vis := b.visibility
	if strings.EqualFold(vis, "hidden") {
		b.mu.RUnlock()
		return
	}
	name := b.name
	if strings.EqualFold(vis, "contacts") || strings.EqualFold(vis, "contacts_only") {
		name = "LANVault Device"
	}
	b.mu.RUnlock()

	payload := BeaconPayload{
		V:        protocol.ProtocolMajor,
		ID:       b.deviceID,
		DeviceID: b.deviceID,
		Name:     name,
		Plat:     b.platform,
		Platform: b.platform,
		Port:     b.port,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	bcasts := netutil.GetBroadcastAddresses()
	for _, bcast := range bcasts {
		conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{
			IP:   bcast,
			Port: protocol.BeaconPort,
		})
		if err != nil {
			continue
		}
		conn.Write(data)
		conn.Close()
	}
}

// BeaconListener listens for UDP beacons on BEACON_PORT
type BeaconListener struct {
	tracker    *Tracker
	conn       *net.UDPConn
	myDeviceID string
	stopChan   chan struct{}
}

// StartBeaconListener starts listening on BEACON_PORT
func StartBeaconListener(tracker *Tracker, bindPort int, myDeviceID ...string) (*BeaconListener, error) {
	addr := &net.UDPAddr{
		Port: bindPort,
		IP:   net.IPv4zero,
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to bind UDP beacon port %d: %w", bindPort, err)
	}

	myID := ""
	if len(myDeviceID) > 0 {
		myID = myDeviceID[0]
	}

	bl := &BeaconListener{
		tracker:    tracker,
		conn:       conn,
		myDeviceID: myID,
		stopChan:   make(chan struct{}),
	}

	go bl.listenLoop()
	return bl, nil
}

func (bl *BeaconListener) listenLoop() {
	buf := make([]byte, 2048)
	for {
		select {
		case <-bl.stopChan:
			return
		default:
			n, remoteAddr, err := bl.conn.ReadFromUDP(buf)
			if err != nil {
				return
			}

			var payload BeaconPayload
			if err := json.Unmarshal(buf[:n], &payload); err != nil {
				continue
			}

			id := payload.ID
			if id == "" {
				id = payload.DeviceID
			}
			plat := payload.Plat
			if plat == "" {
				plat = payload.Platform
			}

			if id == "" || (bl.myDeviceID != "" && id == bl.myDeviceID) {
				continue
			}

			peer := DiscoveredPeer{
				DeviceID: id,
				Name:     payload.Name,
				Platform: plat,
				IPs:      []string{remoteAddr.IP.String()},
				Port:     payload.Port,
				LastSeen: time.Now(),
				Source:   "beacon",
			}
			bl.tracker.UpdatePeer(peer)
		}
	}
}

// Stop closes the UDP listener
func (bl *BeaconListener) Stop() {
	select {
	case <-bl.stopChan:
	default:
		close(bl.stopChan)
		if bl.conn != nil {
			bl.conn.Close()
		}
	}
}
