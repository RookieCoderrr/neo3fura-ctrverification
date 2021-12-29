package network

import (
	"sync"
	"time"

	"github.com/nspcc-dev/neo-go/pkg/network/capability"
)

const (
	maxPoolSize = 200
	connRetries = 3
)

// Discoverer is an interface that is responsible for maintaining
// a healthy connection pool.
type Discoverer interface {
	BackFill(...string)
	Close()
	PoolCount() int
	RequestRemote(int)
	RegisterBadAddr(string)
	RegisterGoodAddr(string, capability.Capabilities)
	RegisterConnectedAddr(string)
	UnregisterConnectedAddr(string)
	UnconnectedPeers() []string
	BadPeers() []string
	GoodPeers() []AddressWithCapabilities
}

// AddressWithCapabilities represents node address with its capabilities.
type AddressWithCapabilities struct {
	Address      string
	Capabilities capability.Capabilities
}

// DefaultDiscovery default implementation of the Discoverer interface.
type DefaultDiscovery struct {
	seeds            []string
	transport        Transporter
	lock             sync.RWMutex
	closeMtx         sync.RWMutex
	dialTimeout      time.Duration
	badAddrs         map[string]bool
	connectedAddrs   map[string]bool
	goodAddrs        map[string]capability.Capabilities
	unconnectedAddrs map[string]int
	attempted        map[string]bool
	isDead           bool
	requestCh        chan int
	pool             chan string
}

// NewDefaultDiscovery returns a new DefaultDiscovery.
func NewDefaultDiscovery(addrs []string, dt time.Duration, ts Transporter) *DefaultDiscovery {
	d := &DefaultDiscovery{
		seeds:            addrs,
		transport:        ts,
		dialTimeout:      dt,
		badAddrs:         make(map[string]bool),
		connectedAddrs:   make(map[string]bool),
		goodAddrs:        make(map[string]capability.Capabilities),
		unconnectedAddrs: make(map[string]int),
		attempted:        make(map[string]bool),
		requestCh:        make(chan int),
		pool:             make(chan string, maxPoolSize),
	}
	go d.run()
	return d
}

func newDefaultDiscovery(addrs []string, dt time.Duration, ts Transporter) Discoverer {
	return NewDefaultDiscovery(addrs, dt, ts)
}

// BackFill implements the Discoverer interface and will backfill the
// the pool with the given addresses.
func (d *DefaultDiscovery) BackFill(addrs ...string) {
	d.lock.Lock()
	for _, addr := range addrs {
		if d.badAddrs[addr] || d.connectedAddrs[addr] ||
			d.unconnectedAddrs[addr] > 0 {
			continue
		}
		d.unconnectedAddrs[addr] = connRetries
		d.pushToPoolOrDrop(addr)
	}
	d.lock.Unlock()
}

// PoolCount returns the number of available node addresses.
func (d *DefaultDiscovery) PoolCount() int {
	return len(d.pool)
}

// pushToPoolOrDrop tries to push address given into the pool, but if the pool
// is already full, it just drops it.
func (d *DefaultDiscovery) pushToPoolOrDrop(addr string) {
	select {
	case d.pool <- addr:
		updatePoolCountMetric(d.PoolCount())
		// ok, queued
	default:
		// whatever
	}
}

// RequestRemote tries to establish a connection with n nodes.
func (d *DefaultDiscovery) RequestRemote(n int) {
	d.closeMtx.RLock()
	if !d.isDead {
		d.requestCh <- n
	}
	d.closeMtx.RUnlock()
}

// RegisterBadAddr registers the given address as a bad address.
func (d *DefaultDiscovery) RegisterBadAddr(addr string) {
	d.lock.Lock()
	d.unconnectedAddrs[addr]--
	if d.unconnectedAddrs[addr] > 0 {
		d.pushToPoolOrDrop(addr)
	} else {
		d.badAddrs[addr] = true
		delete(d.unconnectedAddrs, addr)
		delete(d.goodAddrs, addr)
	}
	d.lock.Unlock()
}

// UnconnectedPeers returns all addresses of unconnected addrs.
func (d *DefaultDiscovery) UnconnectedPeers() []string {
	d.lock.RLock()
	addrs := make([]string, 0, len(d.unconnectedAddrs))
	for addr := range d.unconnectedAddrs {
		addrs = append(addrs, addr)
	}
	d.lock.RUnlock()
	return addrs
}

// BadPeers returns all addresses of bad addrs.
func (d *DefaultDiscovery) BadPeers() []string {
	d.lock.RLock()
	addrs := make([]string, 0, len(d.badAddrs))
	for addr := range d.badAddrs {
		addrs = append(addrs, addr)
	}
	d.lock.RUnlock()
	return addrs
}

// GoodPeers returns all addresses of known good peers (that at least once
// succeeded handshaking with us).
func (d *DefaultDiscovery) GoodPeers() []AddressWithCapabilities {
	d.lock.RLock()
	addrs := make([]AddressWithCapabilities, 0, len(d.goodAddrs))
	for addr, cap := range d.goodAddrs {
		addrs = append(addrs, AddressWithCapabilities{
			Address:      addr,
			Capabilities: cap,
		})
	}
	d.lock.RUnlock()
	return addrs
}

// RegisterGoodAddr registers good known connected address that passed
// handshake successfully.
func (d *DefaultDiscovery) RegisterGoodAddr(s string, c capability.Capabilities) {
	d.lock.Lock()
	d.goodAddrs[s] = c
	delete(d.badAddrs, s)
	d.lock.Unlock()
}

// UnregisterConnectedAddr tells discoverer that this address is no longer
// connected, but it still is considered as good one.
func (d *DefaultDiscovery) UnregisterConnectedAddr(s string) {
	d.lock.Lock()
	delete(d.connectedAddrs, s)
	d.lock.Unlock()
}

// RegisterConnectedAddr tells discoverer that given address is now connected.
func (d *DefaultDiscovery) RegisterConnectedAddr(addr string) {
	d.lock.Lock()
	delete(d.unconnectedAddrs, addr)
	d.connectedAddrs[addr] = true
	d.lock.Unlock()
}

func (d *DefaultDiscovery) tryAddress(addr string) {
	err := d.transport.Dial(addr, d.dialTimeout)
	d.lock.Lock()
	delete(d.attempted, addr)
	d.lock.Unlock()
	if err != nil {
		d.RegisterBadAddr(addr)
		d.RequestRemote(1)
	}
}

// Close stops discoverer pool processing making discoverer almost useless.
func (d *DefaultDiscovery) Close() {
	d.closeMtx.Lock()
	d.isDead = true
	d.closeMtx.Unlock()
	select {
	case <-d.requestCh: // Drain the channel if there is anything there.
	default:
	}
	close(d.requestCh)
}

// run is a goroutine that makes DefaultDiscovery process its queue to connect
// to other nodes.
func (d *DefaultDiscovery) run() {
	var requested, oldRequest, r int
	var ok bool

	for {
		if requested == 0 {
			requested, ok = <-d.requestCh
		}
		oldRequest = requested
		for ok && requested > 0 {
			select {
			case r, ok = <-d.requestCh:
				if requested <= r {
					requested = r
				}
			case addr := <-d.pool:
				updatePoolCountMetric(d.PoolCount())
				d.lock.Lock()
				if !d.connectedAddrs[addr] && !d.attempted[addr] {
					d.attempted[addr] = true
					go d.tryAddress(addr)
					requested--
				}
				d.lock.Unlock()
			default: // Empty pool
				var added int
				d.lock.Lock()
				for _, addr := range d.seeds {
					if !d.connectedAddrs[addr] {
						delete(d.badAddrs, addr)
						d.unconnectedAddrs[addr] = connRetries
						d.pushToPoolOrDrop(addr)
						added++
					}
				}
				d.lock.Unlock()
				// The pool is empty, but all seed nodes are already connected,
				// we can end up in an infinite loop here, so drop the request.
				if added == 0 {
					requested = 0
				}
			}
		}
		if !ok {
			return
		}
		// Special case, no connections after all attempts.
		d.lock.RLock()
		connected := len(d.connectedAddrs)
		d.lock.RUnlock()
		if connected == 0 {
			time.Sleep(d.dialTimeout)
			requested = oldRequest
		}
	}
}
