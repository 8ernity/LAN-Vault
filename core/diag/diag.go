package diag

import (
	"fmt"
	"net"
	"time"

	"lanvault/core/netutil"
	"lanvault/core/protocol"
)

// DiagnosticResult models the outcome of a diagnostic check
type DiagnosticResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// RunDiagnostics runs system diagnostics for network, listener, and ports
func RunDiagnostics() []DiagnosticResult {
	var results []DiagnosticResult

	// 1. Network check
	ifaces, err := netutil.GetEligibleInterfaces()
	if err != nil || len(ifaces) == 0 {
		results = append(results, DiagnosticResult{
			Name:    "Local Network",
			Passed:  false,
			Code:    protocol.LocalErrNoNetwork,
			Message: "No usable local network interface found. Connect to Wi-Fi or turn on Mobile Hotspot.",
		})
	} else {
		var ipList []string
		for _, iface := range ifaces {
			ipList = append(ipList, fmt.Sprintf("%s (%s)", iface.IP.String(), iface.Name))
		}
		results = append(results, DiagnosticResult{
			Name:    "Local Network",
			Passed:  true,
			Message: fmt.Sprintf("Connected to %d local network interface(s)", len(ifaces)),
			Detail:  fmt.Sprintf("%v", ipList),
		})
	}

	// 2. Control Listener check (port 47800)
	l, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", protocol.DefaultPort))
	if err != nil {
		results = append(results, DiagnosticResult{
			Name:    "TCP Control Port",
			Passed:  false,
			Code:    protocol.LocalErrListenerFailed,
			Message: fmt.Sprintf("Port %d could not be opened (already in use or blocked).", protocol.DefaultPort),
			Detail:  err.Error(),
		})
	} else {
		l.Close()
		results = append(results, DiagnosticResult{
			Name:    "TCP Control Port",
			Passed:  true,
			Message: fmt.Sprintf("Port %d is available for transfer listener.", protocol.DefaultPort),
		})
	}

	// 3. UDP Beacon Port check (port 47801)
	u, err := net.ListenUDP("udp4", &net.UDPAddr{Port: protocol.BeaconPort, IP: net.IPv4zero})
	if err != nil {
		results = append(results, DiagnosticResult{
			Name:    "UDP Beacon Port",
			Passed:  false,
			Code:    protocol.LocalErrListenerFailed,
			Message: fmt.Sprintf("UDP Beacon port %d is busy.", protocol.BeaconPort),
			Detail:  err.Error(),
		})
	} else {
		u.Close()
		results = append(results, DiagnosticResult{
			Name:    "UDP Beacon Port",
			Passed:  true,
			Message: fmt.Sprintf("UDP Beacon port %d is available.", protocol.BeaconPort),
		})
	}

	return results
}

// CheckPeerReachability tests direct TCP connection to a peer address
func CheckPeerReachability(addr string, timeout time.Duration) DiagnosticResult {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return DiagnosticResult{
			Name:    "Peer Reachability",
			Passed:  false,
			Message: fmt.Sprintf("Could not connect to %s: %v", addr, err),
			Detail:  "Ensure both devices are on the same Wi-Fi network and AP/client isolation is off.",
		}
	}
	conn.Close()
	return DiagnosticResult{
		Name:    "Peer Reachability",
		Passed:  true,
		Message: fmt.Sprintf("Successfully reached %s", addr),
	}
}
