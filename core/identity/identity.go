package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Identity encapsulates a device's ECDSA P-256 keypair, certificate, and derived IDs
type Identity struct {
	PrivateKey  *ecdsa.PrivateKey
	Certificate *x509.Certificate
	CertDER     []byte
	Fingerprint string // 64 hex chars
	DeviceID    string // 32 hex chars
}

// FingerprintFromDER calculates lowercase hex SHA-256 of certificate DER encoding
func FingerprintFromDER(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// DeviceIDFromFingerprint returns the first 32 hex characters of the fingerprint
func DeviceIDFromFingerprint(fp string) string {
	if len(fp) >= 32 {
		return fp[:32]
	}
	return fp
}

// ShortFingerprintGrouped formats first 8 characters into "abcd 1234"
func ShortFingerprintGrouped(fp string) string {
	if len(fp) < 8 {
		return fp
	}
	return fp[0:4] + " " + fp[4:8]
}

// GenerateNewIdentity creates a fresh self-signed ECDSA P-256 identity valid for 10 years
func GenerateNewIdentity() (*Identity, error) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ECDSA key: %w", err)
	}

	// Random UUID for subject CN
	var uuidBuf [16]byte
	if _, err := rand.Read(uuidBuf[:]); err != nil {
		return nil, err
	}
	uuidBuf[6] = (uuidBuf[6] & 0x0f) | 0x40 // Version 4
	uuidBuf[8] = (uuidBuf[8] & 0x3f) | 0x80 // Variant RFC 4122
	uuidStr := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuidBuf[0:4], uuidBuf[4:6], uuidBuf[6:8], uuidBuf[8:10], uuidBuf[10:16])

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	notBefore := time.Now().Add(-1 * time.Hour) // small skew tolerance
	notAfter := notBefore.Add(10 * 365 * 24 * time.Hour)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: uuidStr,
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse generated certificate: %w", err)
	}

	fp := FingerprintFromDER(derBytes)
	devID := DeviceIDFromFingerprint(fp)

	return &Identity{
		PrivateKey:  privKey,
		Certificate: cert,
		CertDER:     derBytes,
		Fingerprint: fp,
		DeviceID:    devID,
	}, nil
}

// TLSCertificate returns a tls.Certificate suitable for tls.Config
func (id *Identity) TLSCertificate() tls.Certificate {
	return tls.Certificate{
		Certificate: [][]byte{id.CertDER},
		PrivateKey:  id.PrivateKey,
		Leaf:        id.Certificate,
	}
}

// ServerTLSConfig creates mutual TLS 1.3 server config
func (id *Identity) ServerTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{id.TLSCertificate()},
		ClientAuth:   tls.RequireAnyClientCert,
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("tls: client presented no certificate")
			}
			return nil
		},
	}
}

// ClientTLSConfigPinned creates mutual TLS 1.3 client config pinned to expected peer fingerprint (if non-empty)
func (id *Identity) ClientTLSConfigPinned(expectedFP string) *tls.Config {
	cert := id.TLSCertificate()
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("tls: peer presented no certificate")
			}
			if expectedFP != "" {
				peerFP := FingerprintFromDER(rawCerts[0])
				if !strings.EqualFold(peerFP, expectedFP) {
					return fmt.Errorf("tls: peer certificate fingerprint %s does not match expected %s", peerFP, expectedFP)
				}
			}
			return nil
		},
		GetClientCertificate: func(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return &cert, nil
		},
	}
}

// ClientTLSConfig creates mutual TLS 1.3 client config
func (id *Identity) ClientTLSConfig() *tls.Config {
	return id.ClientTLSConfigPinned("")
}

// SavePEM writes identity key and certificate in PEM format to a directory
func (id *Identity) SavePEM(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: id.CertDER,
	})
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), certPEM, 0644); err != nil {
		return err
	}

	keyBytes, err := x509.MarshalECPrivateKey(id.PrivateKey)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), keyPEM, 0600); err != nil {
		return err
	}

	return nil
}

// LoadPEM loads identity key and cert from a directory
func LoadPEM(dir string) (*Identity, error) {
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}

	certBlock, _ := pem.Decode(certBytes)
	if certBlock == nil {
		return nil, errors.New("failed to decode certificate PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, err
	}

	keyBlock, _ := pem.Decode(keyBytes)
	if keyBlock == nil {
		return nil, errors.New("failed to decode key PEM")
	}
	privKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, err
	}

	// Validate that loaded certificate matches private key (F42)
	pubKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pubKey.Equal(&privKey.PublicKey) {
		return nil, errors.New("loaded certificate public key does not match private key")
	}

	fp := FingerprintFromDER(certBlock.Bytes)
	devID := DeviceIDFromFingerprint(fp)

	return &Identity{
		PrivateKey:  privKey,
		Certificate: cert,
		CertDER:     certBlock.Bytes,
		Fingerprint: fp,
		DeviceID:    devID,
	}, nil
}
