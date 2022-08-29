package agent

import (
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ConnStats wraps a net.Conn with statistics.
type ConnStats struct {
	CreatedAt time.Time `json:"created_at,omitempty"`
	Protocol  string    `json:"protocol,omitempty"`

	// RxBytes must be read with atomic.
	RxBytes uint64 `json:"rx_bytes,omitempty"`

	// TxBytes must be read with atomic.
	TxBytes uint64 `json:"tx_bytes,omitempty"`

	net.Conn `json:"-"`
}

var _ net.Conn = new(ConnStats)

func (c *ConnStats) Read(b []byte) (n int, err error) {
	n, err = c.Conn.Read(b)
	atomic.AddUint64(&c.RxBytes, uint64(n))
	return n, err
}

func (c *ConnStats) Write(b []byte) (n int, err error) {
	n, err = c.Conn.Write(b)
	atomic.AddUint64(&c.TxBytes, uint64(n))
	return n, err
}

var _ net.Conn = new(ConnStats)

// Stats records the Agent's network connection statistics for use in
// user-facing metrics and debugging.
type Stats struct {
	sync.RWMutex `json:"-"`
	// ActiveConns are identified by their start time in nanoseconds.
	ActiveConns map[int64]*ConnStats `json:"active_conns,omitempty"`
}

// goConn launches a new connection-processing goroutine, account for
// s.Conns in a thread-safe manner.
func (s *Stats) goConn(conn net.Conn, protocol string, fn func(conn net.Conn)) {
	sc := &ConnStats{
		CreatedAt: time.Now(),
		Protocol:  protocol,
		Conn:      conn,
	}

	key := sc.CreatedAt.UnixNano()

	s.Lock()
	s.ActiveConns[key] = sc
	s.Unlock()

	go func() {
		defer func() {
			s.Lock()
			delete(s.ActiveConns, key)
			s.Unlock()
		}()

		fn(sc)
	}()
}

// StatsReporter periodically accept and records agent stats.
type StatsReporter func(s *Stats)
