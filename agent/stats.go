package agent

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"

	"cdr.dev/slog"
)

// ConnStats wraps a net.Conn with statistics.
type ConnStats struct {
	*ProtocolStats
	net.Conn `json:"-"`
}

var _ net.Conn = new(ConnStats)

func (c *ConnStats) Read(b []byte) (n int, err error) {
	n, err = c.Conn.Read(b)
	atomic.AddInt64(&c.RxBytes, int64(n))
	return n, err
}

func (c *ConnStats) Write(b []byte) (n int, err error) {
	n, err = c.Conn.Write(b)
	atomic.AddInt64(&c.TxBytes, int64(n))
	return n, err
}

type ProtocolStats struct {
	NumConns int64 `json:"num_comms"`

	// RxBytes must be read with atomic.
	RxBytes int64 `json:"rx_bytes"`

	// TxBytes must be read with atomic.
	TxBytes int64 `json:"tx_bytes"`
}

var _ net.Conn = new(ConnStats)

// Stats records the Agent's network connection statistics for use in
// user-facing metrics and debugging.
type Stats struct {
	sync.RWMutex  `json:"-"`
	ProtocolStats map[string]*ProtocolStats `json:"conn_stats,omitempty"`
}

func (s *Stats) Copy() *Stats {
	s.RLock()
	ss := Stats{ProtocolStats: make(map[string]*ProtocolStats, len(s.ProtocolStats))}
	for k, cs := range s.ProtocolStats {
		ss.ProtocolStats[k] = &ProtocolStats{
			NumConns: atomic.LoadInt64(&cs.NumConns),
			RxBytes:  atomic.LoadInt64(&cs.RxBytes),
			TxBytes:  atomic.LoadInt64(&cs.TxBytes),
		}
	}
	s.RUnlock()
	return &ss
}

func (s *Stats) Reset() {
	s.Lock()
	defer s.Unlock()

	for _, ps := range s.ProtocolStats {
		atomic.StoreInt64(&ps.NumConns, 0)
		atomic.StoreInt64(&ps.RxBytes, 0)
		atomic.StoreInt64(&ps.TxBytes, 0)
	}
}

// goConn launches a new connection-processing goroutine, account for
// s.Conns in a thread-safe manner.
func (s *Stats) goConn(conn net.Conn, protocol string, fn func(conn net.Conn)) {
	s.Lock()
	ps, ok := s.ProtocolStats[protocol]
	if !ok {
		ps = &ProtocolStats{}
		s.ProtocolStats[protocol] = ps
	}
	s.Unlock()

	cs := &ConnStats{
		ProtocolStats: ps,
		Conn:          conn,
	}

	atomic.AddInt64(&ps.NumConns, 1)
	go fn(cs)
}

// StatsReporter periodically accept and records agent stats.
// The agent should send incremental stats instead of the cumulative
// value so that SQL queries can efficiently detect activity rates and
// short-lived connections.
//
// E.g., we want to easily query for periods where transfers exceeded 100MB.
type StatsReporter func(
	ctx context.Context,
	log slog.Logger,
	stats func() *Stats,
) (io.Closer, error)
