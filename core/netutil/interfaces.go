package netutil

import (
	"net"
	"strings"
)

// NetworkInterface holds IP and interface name for local peer discovery and binding
type NetworkInterface struct {
	Name      string
	IP        net.IP
	IsLoopback bool
}

// GetEligibleInterfaces returns non-loopback IPv4 addresses suitable for local sharing
func GetEligibleInterfaces() ([]NetworkInterface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var results []NetworkInterface
	for _, iface := range ifaces {
		// Skip interfaces that are down
		if (iface.Flags & net.FlagUp) == 0 {
			continue
		}
		// Skip known virtual adapters (Hyper-V, VirtualBox, VMware, WSL, Tailscale, ZeroTier)
		nameLower := strings.ToLower(iface.Name)
		if strings.Contains(nameLower, "virtual") ||
			strings.Contains(nameLower, "vbox") ||
			strings.Contains(nameLower, "vmware") ||
			strings.Contains(nameLower, "wsl") ||
			strings.Contains(nameLower, "tailscale") ||
			strings.Contains(nameLower, "zerotier") {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// We use IPv4 for standard local network and hotspot scenarios in v1.0
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}

			// Ignore link-local (169.254.x.x)
			if ip.IsLinkLocalUnicast() {
				continue
			}

			results = append(results, NetworkInterface{
				Name:       iface.Name,
				IP:         ip,
				IsLoopback: false,
			})
		}
	}

	return results, nil
}

// GetLocalIPStrings returns slice of IP strings for QR code and advertisements
func GetLocalIPStrings() []string {
	ifaces, err := GetEligibleInterfaces()
	if err != nil || len(ifaces) == 0 {
		return []string{"127.0.0.1"}
	}
	var ips []string
	for _, iface := range ifaces {
		ips = append(ips, iface.IP.String())
	}
	return ips
}

// GetBroadcastAddresses returns 255.255.255.255 plus all active interface subnet broadcast addresses
func GetBroadcastAddresses() []net.IP {
	bcasts := []net.IP{net.IPv4bcast}
	seen := map[string]bool{net.IPv4bcast.String(): true}

	ifaces, err := net.Interfaces()
	if err != nil {
		return bcasts
	}

	for _, iface := range ifaces {
		if (iface.Flags&net.FlagUp) == 0 || (iface.Flags&net.FlagBroadcast) == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP == nil || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
				continue
			}
			ip := ipNet.IP.To4()
			mask := ipNet.Mask
			if len(mask) == 4 {
				bcast := net.IPv4(
					ip[0]|^mask[0],
					ip[1]|^mask[1],
					ip[2]|^mask[2],
					ip[3]|^mask[3],
				)
				str := bcast.String()
				if !seen[str] {
					seen[str] = true
					bcasts = append(bcasts, bcast)
				}
			}
		}
	}
	return bcasts
}
