package service

import (
	"crynux_as/config"
	"sync"
)

// ProviderKeySlotManager tracks process-local active Shared API Key concurrency slots.
type ProviderKeySlotManager struct {
	mu       sync.Mutex
	active   map[uint]uint64
	maxSlots uint64
}

func NewProviderKeySlotManager(maxSlots uint64) *ProviderKeySlotManager {
	return &ProviderKeySlotManager{
		active:   make(map[uint]uint64),
		maxSlots: maxSlots,
	}
}

var (
	providerKeySlots     *ProviderKeySlotManager
	providerKeySlotsOnce sync.Once
)

func InitProviderKeySlots(maxSlots uint64) {
	providerKeySlotsOnce.Do(func() {
		providerKeySlots = NewProviderKeySlotManager(maxSlots)
	})
}

func GetProviderKeySlots() *ProviderKeySlotManager {
	if providerKeySlots == nil {
		InitProviderKeySlots(config.GetConfig().Provider.MaxConcurrencyPerKey)
	}
	return providerKeySlots
}

func (m *ProviderKeySlotManager) Available(keyID uint) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active[keyID] < m.maxSlots
}

func (m *ProviderKeySlotManager) ActiveCount(keyID uint) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active[keyID]
}

// Reserve increases the active slot count for keyID when capacity remains.
func (m *ProviderKeySlotManager) Reserve(keyID uint) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active[keyID] >= m.maxSlots {
		return false
	}
	m.active[keyID]++
	return true
}

// Release decreases the active slot count for keyID. It is idempotent at zero.
func (m *ProviderKeySlotManager) Release(keyID uint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active[keyID] == 0 {
		return
	}
	m.active[keyID]--
	if m.active[keyID] == 0 {
		delete(m.active, keyID)
	}
}

func (m *ProviderKeySlotManager) MaxSlots() uint64 {
	return m.maxSlots
}

func (m *ProviderKeySlotManager) ResetForTest() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = make(map[uint]uint64)
}
