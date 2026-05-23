// Package pool manages a pool of worker hosts for scheduling.
package worker

import (
	"strings"
	"sync"
)

// Pool tracks worker host capacity and load for dispatch decisions.
type Pool struct {
	mu                   sync.Mutex
	hosts                []string
	maxConcurrentPerHost int
	running              map[string]int // host -> count of running agents
}

// NewPool creates a worker pool.
// If hosts is empty, all work runs locally.
func NewPool(hosts []string, maxConcurrentPerHost int) *Pool {
	return &Pool{
		hosts:                hosts,
		maxConcurrentPerHost: maxConcurrentPerHost,
		running:              make(map[string]int),
	}
}

// IsSSH returns true if SSH hosts are configured.
func (p *Pool) IsSSH() bool {
	return len(p.hosts) > 0
}

// SelectHost picks a host with available capacity.
// preferredHost is used if it has capacity (for retry stability).
// Returns host string (empty for local), or error if no capacity.
func (p *Pool) SelectHost(preferredHost string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.hosts) == 0 {
		return "", true // local mode
	}

	// Try preferred host first
	if preferredHost != "" {
		for _, h := range p.hosts {
			if h == preferredHost && p.hostHasCapacity(h) {
				return h, true
			}
		}
	}

	// Find least-loaded host with capacity
	var bestHost string
	bestCount := -1
	for _, h := range p.hosts {
		if !p.hostHasCapacity(h) {
			continue
		}
		count := p.running[h]
		if bestHost == "" || count < bestCount {
			bestHost = h
			bestCount = count
		}
	}

	if bestHost == "" {
		return "", false // no capacity
	}
	return bestHost, true
}

// Acquire marks a slot as used on a host.
func (p *Pool) Acquire(host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running[host]++
}

// Release marks a slot as freed on a host.
func (p *Pool) Release(host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running[host] > 0 {
		p.running[host]--
	}
}

// HostCounts returns a snapshot of per-host running counts.
func (p *Pool) HostCounts() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]int, len(p.running))
	for k, v := range p.running {
		out[k] = v
	}
	return out
}

func (p *Pool) hostHasCapacity(host string) bool {
	if p.maxConcurrentPerHost <= 0 {
		return true
	}
	return p.running[host] < p.maxConcurrentPerHost
}

// LauncherForHost returns the appropriate Launcher for a host.
// Empty host returns a local launcher.
func LauncherForHost(host string) Launcher {
	host = strings.TrimSpace(host)
	if host == "" {
		return NewLocalLauncher()
	}
	return NewSSHLauncher(host)
}
