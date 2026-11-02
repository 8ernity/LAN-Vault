package pairing

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"lanvault/core/protocol"
)

var (
	ErrInvalidScheme      = errors.New("invalid URI scheme, expected lanvault://pair")
	ErrUnsupportedVersion = errors.New("unsupported pairing version")
	ErrInvalidFingerprint = errors.New("invalid fingerprint length in QR URI")
	ErrTokenExpired       = errors.New("pairing token expired")
	ErrTokenInvalid       = errors.New("invalid pairing token")
	ErrMaxAttemptsExceeded = errors.New("maximum pairing attempts exceeded")
)

// URIParams represents parsed contents of a lanvault://pair QR URI
type URIParams struct {
	Version     int
	Fingerprint string
	IPs         []string
	Port        int
	Name        string
	Token       []byte
	RawToken    string
}

// GenerateURI builds a lanvault://pair URI
func GenerateURI(version int, fp string, ips []string, port int, name string, token []byte) string {
	tokStr := base64.RawURLEncoding.EncodeToString(token)
	ipStr := strings.Join(ips, ",")
	v := url.Values{}
	v.Set("v", strconv.Itoa(version))
	v.Set("fp", fp)
	v.Set("ip", ipStr)
	v.Set("port", strconv.Itoa(port))
	v.Set("name", name)
	v.Set("tok", tokStr)

	// Build custom scheme URI
	return fmt.Sprintf("lanvault://pair?%s", v.Encode())
}

// ParseURI parses a lanvault://pair URI string
func ParseURI(rawURI string) (*URIParams, error) {
	if !strings.HasPrefix(rawURI, "lanvault://pair?") {
		return nil, ErrInvalidScheme
	}
	queryStr := rawURI[len("lanvault://pair?"):]
	values, err := url.ParseQuery(queryStr)
	if err != nil {
		return nil, err
	}

	vStr := values.Get("v")
	v, err := strconv.Atoi(vStr)
	if err != nil || v != protocol.ProtocolMajor {
		return nil, ErrUnsupportedVersion
	}

	fp := values.Get("fp")
	if len(fp) != 64 {
		return nil, ErrInvalidFingerprint
	}

	ipStr := values.Get("ip")
	ips := strings.Split(ipStr, ",")

	portStr := values.Get("port")
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return nil, errors.New("invalid port")
	}

	name := values.Get("name")
	tokStr := values.Get("tok")
	
	// Support both padded and unpadded URL base64
	tokBytes, err := base64.RawURLEncoding.DecodeString(tokStr)
	if err != nil {
		tokBytes, err = base64.URLEncoding.DecodeString(tokStr)
		if err != nil {
			return nil, fmt.Errorf("failed to decode token: %w", err)
		}
	}

	return &URIParams{
		Version:     v,
		Fingerprint: fp,
		IPs:         ips,
		Port:        port,
		Name:        name,
		Token:       tokBytes,
		RawToken:    tokStr,
	}, nil
}

// TokenSession tracks active pairing session on displayer
type TokenSession struct {
	Token     []byte
	CreatedAt time.Time
	Attempts  int
}

// Manager manages pairing session lifecycle on the displaying device
type Manager struct {
	mu           sync.Mutex
	active       *TokenSession
	ttl          time.Duration
	maxAttempts  int
}

// NewManager creates a pairing manager
func NewManager() *Manager {
	return &Manager{
		ttl:         time.Duration(protocol.PairTokenTTLSec) * time.Second,
		maxAttempts: protocol.PairMaxFailedAttempts,
	}
}

// CreateNewToken generates 16 random bytes and starts/refreshes the session
func (m *Manager) CreateNewToken() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tok := make([]byte, protocol.PairTokenBytes)
	if _, err := rand.Read(tok); err != nil {
		return nil, err
	}

	m.active = &TokenSession{
		Token:     tok,
		CreatedAt: time.Now(),
		Attempts:  0,
	}
	return tok, nil
}

// VerifyToken verifies incoming token from scanner using constant-time comparison
func (m *Manager) VerifyToken(receivedTokB64 string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.active == nil {
		return ErrTokenInvalid
	}

	if time.Since(m.active.CreatedAt) > m.ttl {
		m.active = nil
		return ErrTokenExpired
	}

	if m.active.Attempts >= m.maxAttempts {
		m.active = nil
		return ErrMaxAttemptsExceeded
	}

	recBytes, err := base64.RawURLEncoding.DecodeString(receivedTokB64)
	if err != nil {
		recBytes, err = base64.URLEncoding.DecodeString(receivedTokB64)
		if err != nil {
			m.active.Attempts++
			return ErrTokenInvalid
		}
	}

	if len(recBytes) != len(m.active.Token) || subtle.ConstantTimeCompare(recBytes, m.active.Token) != 1 {
		m.active.Attempts++
		if m.active.Attempts >= m.maxAttempts {
			m.active = nil
			return ErrMaxAttemptsExceeded
		}
		return ErrTokenInvalid
	}

	// Single-use token: invalidate on success
	m.active = nil
	return nil
}

// CancelSession invalidates current token
func (m *Manager) CancelSession() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = nil
}
