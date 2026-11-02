package identity

import (
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func findProjectRoot(t *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "docs", "protocol-constants.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find project root from %s", wd)
		}
		dir = parent
	}
}

func TestIdentityTestVector(t *testing.T) {
	root := findProjectRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testvectors", "identity.json"))
	if err != nil {
		t.Fatalf("read identity.json failed: %v", err)
	}

	var data struct {
		CertDERHex             string `json:"certificate_der_hex"`
		Fingerprint            string `json:"fingerprint"`
		DeviceID               string `json:"device_id"`
		ShortFingerprintGrouped string `json:"short_fingerprint_grouped"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal identity.json failed: %v", err)
	}

	derBytes, err := hex.DecodeString(data.CertDERHex)
	if err != nil {
		t.Fatalf("decode cert_der_hex failed: %v", err)
	}

	fp := FingerprintFromDER(derBytes)
	if fp != data.Fingerprint {
		t.Errorf("fingerprint mismatch: got %s, want %s", fp, data.Fingerprint)
	}

	devID := DeviceIDFromFingerprint(fp)
	if devID != data.DeviceID {
		t.Errorf("device_id mismatch: got %s, want %s", devID, data.DeviceID)
	}

	shortFp := ShortFingerprintGrouped(fp)
	if shortFp != data.ShortFingerprintGrouped {
		t.Errorf("short_fingerprint_grouped mismatch: got %s, want %s", shortFp, data.ShortFingerprintGrouped)
	}
}

func TestIdentityGenerationAndTLS13Handshake(t *testing.T) {
	serverID, err := GenerateNewIdentity()
	if err != nil {
		t.Fatalf("GenerateNewIdentity for server failed: %v", err)
	}

	clientID, err := GenerateNewIdentity()
	if err != nil {
		t.Fatalf("GenerateNewIdentity for client failed: %v", err)
	}

	if len(serverID.Fingerprint) != 64 {
		t.Errorf("server fingerprint length %d != 64", len(serverID.Fingerprint))
	}
	if len(serverID.DeviceID) != 32 {
		t.Errorf("server device_id length %d != 32", len(serverID.DeviceID))
	}

	// Start local TLS 1.3 listener
	serverTLS := serverID.ServerTLSConfig()
	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	if err != nil {
		t.Fatalf("tls.Listen failed: %v", err)
	}
	defer listener.Close()

	errChan := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer conn.Close()
		tlsConn, ok := conn.(*tls.Conn)
		if !ok {
			return
		}
		if err := tlsConn.Handshake(); err != nil {
			errChan <- err
			return
		}
		state := tlsConn.ConnectionState()
		if state.Version != tls.VersionTLS13 {
			t.Errorf("TLS version is %x, expected TLS 1.3 (%x)", state.Version, tls.VersionTLS13)
		}
		if len(state.PeerCertificates) == 0 {
			t.Errorf("server saw no client peer certificates")
		} else {
			clientFP := FingerprintFromDER(state.PeerCertificates[0].Raw)
			if clientFP != clientID.Fingerprint {
				t.Errorf("client fingerprint mismatch: got %s, want %s", clientFP, clientID.Fingerprint)
			}
		}
		errChan <- nil
	}()

	clientTLS := clientID.ClientTLSConfig()
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	tlsConn := tls.Client(conn, clientTLS)
	if err := tlsConn.Handshake(); err != nil {
		t.Fatalf("client handshake failed: %v", err)
	}
	state := tlsConn.ConnectionState()
	if state.Version != tls.VersionTLS13 {
		t.Errorf("client saw TLS version %x, expected %x", state.Version, tls.VersionTLS13)
	}
	if len(state.PeerCertificates) == 0 {
		t.Errorf("client saw no server peer certificates")
	} else {
		serverFP := FingerprintFromDER(state.PeerCertificates[0].Raw)
		if serverFP != serverID.Fingerprint {
			t.Errorf("server fingerprint mismatch: got %s, want %s", serverFP, serverID.Fingerprint)
		}
	}

	if err := <-errChan; err != nil {
		t.Fatalf("server side handshake error: %v", err)
	}
}
