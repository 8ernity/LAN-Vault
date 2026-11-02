package pairing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

func TestPairingURITestVector(t *testing.T) {
	root := findProjectRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testvectors", "pairing_uri.json"))
	if err != nil {
		t.Fatalf("read pairing_uri.json failed: %v", err)
	}

	var data struct {
		QRURIExample string `json:"qr_uri_example"`
		Expected     struct {
			V        string   `json:"v"`
			FP       string   `json:"fp"`
			IP       []string `json:"ip"`
			Port     int      `json:"port"`
			Name     string   `json:"name"`
			TokBytes int      `json:"tok_bytes"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal pairing_uri.json failed: %v", err)
	}

	params, err := ParseURI(data.QRURIExample)
	if err != nil {
		t.Fatalf("ParseURI failed: %v", err)
	}

	if params.Fingerprint != data.Expected.FP {
		t.Errorf("fingerprint mismatch: got %s, want %s", params.Fingerprint, data.Expected.FP)
	}
	if !reflect.DeepEqual(params.IPs, data.Expected.IP) {
		t.Errorf("IPs mismatch: got %v, want %v", params.IPs, data.Expected.IP)
	}
	if params.Port != data.Expected.Port {
		t.Errorf("port mismatch: got %d, want %d", params.Port, data.Expected.Port)
	}
	if params.Name != data.Expected.Name {
		t.Errorf("name mismatch: got %s, want %s", params.Name, data.Expected.Name)
	}
	if len(params.Token) != data.Expected.TokBytes {
		t.Errorf("token bytes mismatch: got %d, want %d", len(params.Token), data.Expected.TokBytes)
	}

	// Verify regenerated URI parses back identically
	regenerated := GenerateURI(params.Version, params.Fingerprint, params.IPs, params.Port, params.Name, params.Token)
	params2, err := ParseURI(regenerated)
	if err != nil {
		t.Fatalf("ParseURI of regenerated failed: %v", err)
	}
	if !reflect.DeepEqual(params, params2) {
		t.Errorf("roundtrip mismatch:\ngot:  %+v\nwant: %+v", params2, params)
	}
}

func TestManagerLifecycle(t *testing.T) {
	mgr := NewManager()
	tok, err := mgr.CreateNewToken()
	if err != nil {
		t.Fatalf("CreateNewToken failed: %v", err)
	}

	uri := GenerateURI(1, "1234567890123456789012345678901234567890123456789012345678901234", []string{"192.168.1.10"}, 47800, "Device", tok)
	parsed, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI failed: %v", err)
	}

	// Verify wrong token fails
	if err := mgr.VerifyToken("wrongTokenABCDEF"); err == nil {
		t.Errorf("expected error for wrong token, got nil")
	}

	// Verify correct token succeeds
	if err := mgr.VerifyToken(parsed.RawToken); err != nil {
		t.Fatalf("VerifyToken failed with valid token: %v", err)
	}

	// Verify token is single-use (fails on second attempt)
	if err := mgr.VerifyToken(parsed.RawToken); err == nil {
		t.Errorf("expected error for reused token, got nil")
	}
}
