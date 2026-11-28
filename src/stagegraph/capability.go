package stagegraph

import (
	"strings"
	"sync"
)

// Capability records whether a Jenkins controller exposes the optional
// pipeline-graph-view tree API and which endpoint path it supports.
type Capability uint8

const (
	CapabilityUnknown Capability = iota
	CapabilityOverview
	CapabilityLegacy
	CapabilityUnavailable
)

// CapabilityCache stores per-server pipeline-graph-view discovery results.
// It is safe for concurrent use by background build-fetch and poll-refresh
// commands, and Reset clears all entries when the active server changes.
type CapabilityCache struct {
	mu      sync.RWMutex
	entries map[string]Capability
}

// NewCapabilityCache returns an empty per-server capability cache.
func NewCapabilityCache() *CapabilityCache {
	return &CapabilityCache{
		entries: make(map[string]Capability),
	}
}

// Get returns the cached capability for serverKey, or CapabilityUnknown when
// unprobed or when c is nil.
func (c *CapabilityCache) Get(serverKey string) Capability {
	if c == nil {
		return CapabilityUnknown
	}
	key := normalizeServerKey(serverKey)
	c.mu.RLock()
	capState := c.entries[key]
	c.mu.RUnlock()
	return capState
}

// Set records the discovered capability for serverKey.
func (c *CapabilityCache) Set(serverKey string, capability Capability) {
	if c == nil {
		return
	}
	key := normalizeServerKey(serverKey)
	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[string]Capability)
	}
	c.entries[key] = capability
	c.mu.Unlock()
}

// Reset discards all cached server capabilities.
func (c *CapabilityCache) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entries = make(map[string]Capability)
	c.mu.Unlock()
}

func normalizeServerKey(key string) string {
	return strings.TrimRight(strings.TrimSpace(key), "/")
}
