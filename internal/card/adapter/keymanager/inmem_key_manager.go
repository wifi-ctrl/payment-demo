package keymanager

import (
	"crypto/rand"
	"fmt"
	"sync"

	"payment-demo/internal/card/domain/port"
)

type dekEntry struct {
	dek    []byte
	status string // "active", "retiring", "retired"
}

// InMemKeyManager Demo 用内存密钥管理器
type InMemKeyManager struct {
	mu             sync.RWMutex
	deks           map[int]*dekEntry
	currentVersion int
	hmacKey        []byte
}

var _ port.KeyManager = (*InMemKeyManager)(nil)

func NewInMemKeyManager() *InMemKeyManager {
	dek := mustRandBytes(32)
	return &InMemKeyManager{
		deks: map[int]*dekEntry{
			1: {dek: dek, status: "active"},
		},
		currentVersion: 1,
		hmacKey:        mustRandBytes(32),
	}
}

func (m *InMemKeyManager) CurrentDEK() ([]byte, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry := m.deks[m.currentVersion]
	return entry.dek, m.currentVersion, nil
}

func (m *InMemKeyManager) DEKByVersion(version int) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.deks[version]
	if !ok {
		return nil, fmt.Errorf("DEK version %d not found", version)
	}
	return entry.dek, nil
}

func (m *InMemKeyManager) HMACKey() ([]byte, error) {
	return m.hmacKey, nil
}

func (m *InMemKeyManager) RotateDEK() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.deks[m.currentVersion]; ok {
		old.status = "retiring"
	}
	newVersion := m.currentVersion + 1
	m.deks[newVersion] = &dekEntry{
		dek:    mustRandBytes(32),
		status: "active",
	}
	m.currentVersion = newVersion
	return newVersion, nil
}

func (m *InMemKeyManager) RetireDEK(version int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.deks[version]
	if !ok {
		return fmt.Errorf("DEK version %d not found", version)
	}
	entry.status = "retired"
	return nil
}

func (m *InMemKeyManager) ListVersions() ([]port.KeyVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var versions []port.KeyVersion
	for v, entry := range m.deks {
		versions = append(versions, port.KeyVersion{
			Version: v,
			Status:  entry.status,
		})
	}
	return versions, nil
}

func mustRandBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("failed to generate random bytes: %v", err))
	}
	return b
}
