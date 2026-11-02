package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lanvault/core/diag"
	"lanvault/core/identity"
	"lanvault/core/netutil"
	"lanvault/core/pairing"
	"lanvault/core/protocol"
	"lanvault/core/store"
	"lanvault/core/transfer"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	appDir, _ := os.UserConfigDir()
	dataDir := filepath.Join(appDir, "LANVault")
	os.MkdirAll(dataDir, 0700)

	id, err := identity.LoadPEM(dataDir)
	if err != nil {
		id, err = identity.GenerateNewIdentity()
		if err != nil {
			fmt.Printf("Failed to generate identity: %v\n", err)
			os.Exit(1)
		}
		id.SavePEM(dataDir)
	}

	st, err := store.OpenStore(filepath.Join(dataDir, "lanvault.db"))
	if err != nil {
		fmt.Printf("Failed to open local store: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	cmd := os.Args[1]

	switch cmd {
	case "id":
		ips := netutil.GetLocalIPStrings()
		fmt.Printf("Device ID:         %s\n", id.DeviceID)
		fmt.Printf("Fingerprint:       %s\n", id.Fingerprint)
		fmt.Printf("Short Fingerprint: %s\n", identity.ShortFingerprintGrouped(id.Fingerprint))
		fmt.Printf("Local IPs:         %s\n", strings.Join(ips, ", "))

		mgr := pairing.NewManager()
		tok, _ := mgr.CreateNewToken()
		hostname, _ := os.Hostname()
		uri := pairing.GenerateURI(protocol.ProtocolMajor, id.Fingerprint, ips, protocol.DefaultPort, hostname, tok)
		fmt.Printf("\nPairing QR URI:\n%s\n", uri)

	case "diag":
		fmt.Println("Running LANVault diagnostics...")
		results := diag.RunDiagnostics()
		for _, r := range results {
			status := "[PASS]"
			if !r.Passed {
				status = "[FAIL]"
			}
			fmt.Printf("%-6s %-20s %s\n", status, r.Name, r.Message)
			if r.Detail != "" {
				fmt.Printf("       Detail: %s\n", r.Detail)
			}
		}

	case "listen":
		listenFlags := flag.NewFlagSet("listen", flag.ExitOnError)
		port := listenFlags.Int("port", protocol.DefaultPort, "TCP listen port")
		destDir := listenFlags.String("dir", filepath.Join(dataDir, "Received"), "Directory to save received files")
		listenFlags.Parse(os.Args[2:])

		os.MkdirAll(*destDir, 0755)
		serverTLS := id.ServerTLSConfig()
		listener, err := tls.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", *port), serverTLS)
		if err != nil {
			fmt.Printf("Failed to bind port %d: %v\n", *port, err)
			os.Exit(1)
		}
		defer listener.Close()

		fmt.Printf("Listening on 0.0.0.0:%d (saving to %s)\n", *port, *destDir)
		fmt.Printf("Device Fingerprint: %s\n", id.Fingerprint)
		fmt.Println("Waiting for incoming transfers... (Press Ctrl+C to stop)")

		onDecision := func(offer protocol.OfferMessage, peerName, peerFP string) (bool, string) {
			fmt.Printf("\n[OFFER] Incoming transfer from '%s' (%s)\n", peerName, identity.ShortFingerprintGrouped(peerFP))
			fmt.Printf("        Files: %d, Total bytes: %d, Chunk size: %d bytes\n", offer.FileCount, offer.TotalBytes, offer.ChunkSize)
			fmt.Printf("        Accepting automatically into: %s\n", *destDir)
			return true, *destDir
		}

		for {
			conn, err := listener.Accept()
			if err != nil {
				fmt.Printf("Accept error: %v\n", err)
				break
			}
			go func(c net.Conn) {
				defer c.Close()
				if err := transfer.HandleIncomingControl(c, id, st, onDecision, listener); err != nil {
					fmt.Printf("Transfer error: %v\n", err)
				} else {
					fmt.Println("\n[SUCCESS] Transfer completed successfully!")
				}
			}(conn)
		}

	case "send":
		if len(os.Args) < 4 {
			fmt.Println("Usage: lanvault-cli send <target_ip:port> <path1> [<path2>...]")
			os.Exit(1)
		}
		targetAddr := os.Args[2]
		paths := os.Args[3:]

		fmt.Printf("Scanning items to send...\n")
		items, totalFiles, totalBytes, manifestHash, err := transfer.ScanSourceItems(paths)
		if err != nil {
			fmt.Printf("Scan failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Selection: %d files (%d bytes), manifest hash: %.16s...\n", totalFiles, totalBytes, manifestHash)
		fmt.Printf("Connecting to %s...\n", targetAddr)

		transferID := fmt.Sprintf("cli-%d", time.Now().UnixNano())
		hostname, _ := os.Hostname()

		startTime := time.Now()
		var lastBytes int64 = 0
		var lastTime = startTime

		progressCb := func(bytesAcked int64, total int64, filesDone int, totalFiles int) {
			now := time.Now()
			elapsed := now.Sub(lastTime).Seconds()
			speed := float64(bytesAcked-lastBytes) / (elapsed * 1024 * 1024)
			pct := float64(bytesAcked) / float64(total) * 100.0
			fmt.Printf("\rProgress: %.1f%% (%d/%d bytes) | %d/%d files | %.2f MB/s", pct, bytesAcked, total, filesDone, totalFiles, speed)
			lastBytes = bytesAcked
			lastTime = now
		}

		doneOk, err := transfer.StartSend(
			targetAddr,
			id,
			hostname,
			transferID,
			items,
			totalFiles,
			totalBytes,
			manifestHash,
			protocol.ChunkSizeDefault,
			progressCb,
		)
		if err != nil {
			fmt.Printf("\nSend failed: %v\n", err)
			os.Exit(1)
		}

		totalDuration := time.Since(startTime).Seconds()
		avgSpeed := (float64(doneOk.Bytes) / (1024 * 1024)) / totalDuration
		fmt.Printf("\n\nTransfer completed! %d files, %d bytes in %.2fs (avg %.2f MB/s)\n", doneOk.FilesCompleted, doneOk.Bytes, totalDuration, avgSpeed)

	default:
		printUsage()
	}
}

func printUsage() {
	fmt.Println("LANVault Developer CLI")
	fmt.Println("Usage:")
	fmt.Println("  lanvault-cli id                       Show identity, fingerprint, local IPs, and pairing QR URI")
	fmt.Println("  lanvault-cli diag                     Run local network and port diagnostics")
	fmt.Println("  lanvault-cli listen [--dir <path>]    Listen for incoming transfers")
	fmt.Println("  lanvault-cli send <ip:port> <paths>   Send files/folders to a receiver")
}
