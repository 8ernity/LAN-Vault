package discovery

import (
	"testing"
	"time"
)

func TestTrackerLifecycleAndExpiry(t *testing.T) {
	tracker := NewTracker()

	peer1 := DiscoveredPeer{
		DeviceID: "dev-001",
		Name:     "Alice's Phone",
		Platform: "android",
		IPs:      []string{"192.168.1.50"},
		Port:     47800,
		Source:   "beacon",
	}

	notifiedCount := 0
	tracker.Subscribe(func(peers []DiscoveredPeer) {
		notifiedCount++
	})

	tracker.UpdatePeer(peer1)
	if len(tracker.ListPeers()) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(tracker.ListPeers()))
	}
	if notifiedCount != 1 {
		t.Errorf("listener not notified on update")
	}

	// Update with new IP for same device_id
	peer1Updated := DiscoveredPeer{
		DeviceID: "dev-001",
		Name:     "Alice's Phone (Renamed)",
		Platform: "android",
		IPs:      []string{"192.168.1.51"},
		Port:     47800,
		Source:   "beacon",
	}
	tracker.UpdatePeer(peer1Updated)
	peers := tracker.ListPeers()
	if len(peers) != 1 {
		t.Fatalf("peer should be deduplicated by device_id, got %d", len(peers))
	}
	if peers[0].Name != "Alice's Phone (Renamed)" {
		t.Errorf("name not updated: %s", peers[0].Name)
	}
	if len(peers[0].IPs) != 2 {
		t.Errorf("expected 2 merged IPs, got %v", peers[0].IPs)
	}

	// Expiry test
	time.Sleep(10 * time.Millisecond)
	tracker.ExpireStalePeers(5 * time.Millisecond)
	if len(tracker.ListPeers()) != 0 {
		t.Errorf("expected peer to be expired, got %d", len(tracker.ListPeers()))
	}
}
