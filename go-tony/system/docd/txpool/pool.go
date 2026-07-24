// Package txpool provides pre-fetched transaction ID pooling for docd.
//
// docd caches transaction IDs from logd to reduce round trips when
// controllers need to create multi-participant transactions.
package txpool

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// ioTimeout bounds each request/response round trip to logd (the hello handshake and each
// newtx fetch). Get holds p.mu across the fetch, so without a deadline a stalled or half-open
// logd would wedge every pooled tx allocation server-wide, forever (issue rgn4amsw). A
// deadline turns a hung read/write into an error, which the caller turns into a reconnect.
const ioTimeout = 10 * time.Second

// Pool manages pre-fetched transaction IDs from logd.
// It maintains a connection to logd and pools TxIDs by participant count.
type Pool struct {
	logdAddr string
	log      *slog.Logger

	mu        sync.Mutex
	conn      net.Conn
	decoder   *stream.Decoder
	pools     map[int][]int64 // participants -> available TxIDs
	poolSize  int             // target count per participant count
	connected bool

	// refillCh signals the background refiller to top a participant count back up
	// to poolSize after a Get drains one, so Gets keep being served from cache.
	refillCh chan int

	// For shutdown
	done   chan struct{}
	closed bool

	ioTimeout time.Duration // per round-trip deadline to logd (see ioTimeout const)
}

// Config holds configuration for the transaction pool.
type Config struct {
	LogdAddr  string        // Address of logd server
	PoolSize  int           // Number of TxIDs to pre-fetch per participant count (default: 10)
	Log       *slog.Logger  // Logger (optional)
	IOTimeout time.Duration // Per round-trip deadline to logd (default: ioTimeout)
}

// New creates a new transaction ID pool.
// It does not connect immediately - call Connect or let Get auto-connect.
func New(cfg *Config) *Pool {
	poolSize := cfg.PoolSize
	if poolSize <= 0 {
		poolSize = 10
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	ioT := cfg.IOTimeout
	if ioT <= 0 {
		ioT = ioTimeout
	}

	p := &Pool{
		logdAddr:  cfg.LogdAddr,
		log:       log.With("component", "txpool"),
		pools:     make(map[int][]int64),
		poolSize:  poolSize,
		refillCh:  make(chan int, 64),
		done:      make(chan struct{}),
		ioTimeout: ioT,
	}
	go p.refillLoop()
	return p
}

// Connect establishes connection to logd with retry.
// This is called automatically by Get if not connected.
func (p *Pool) Connect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.connected {
		return nil
	}

	return p.connectLocked(ctx)
}

// connectLocked establishes connection with retry (must hold mutex).
func (p *Pool) connectLocked(ctx context.Context) error {
	backoff := 100 * time.Millisecond
	maxBackoff := 5 * time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.done:
			return fmt.Errorf("pool closed")
		default:
		}

		conn, err := net.DialTimeout("tcp", p.logdAddr, 5*time.Second)
		if err != nil {
			p.log.Debug("failed to connect to logd, retrying", "addr", p.logdAddr, "error", err, "backoff", backoff)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-p.done:
				return fmt.Errorf("pool closed")
			case <-time.After(backoff):
			}

			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		// Create decoder for responses
		decoder, err := stream.NewDecoder(conn, stream.WithBrackets())
		if err != nil {
			conn.Close()
			return fmt.Errorf("failed to create decoder: %w", err)
		}

		// Bound the hello handshake: a peer that completes the TCP dial but never answers
		// hello must not block forever under p.mu.
		_ = conn.SetDeadline(time.Now().Add(p.ioTimeout))

		// Send hello
		if err := p.sendHello(conn); err != nil {
			conn.Close()
			return fmt.Errorf("hello failed: %w", err)
		}

		// Read hello response
		resp, err := p.readResponse(decoder)
		if err != nil {
			conn.Close()
			return fmt.Errorf("failed to read hello response: %w", err)
		}
		if resp.Error != nil {
			conn.Close()
			return fmt.Errorf("hello error: %s", resp.Error.Message)
		}
		_ = conn.SetDeadline(time.Time{}) // clear; fetchTxID sets its own per round trip

		p.conn = conn
		p.decoder = decoder
		p.connected = true
		p.log.Info("connected to logd", "addr", p.logdAddr)
		return nil
	}
}

// sendHello sends the hello message to logd.
func (p *Pool) sendHello(conn net.Conn) error {
	req := &api.SessionRequest{
		Hello: &api.Hello{
			ClientID: "docd-txpool",
		},
	}
	return p.sendRequest(conn, req)
}

// sendRequest sends a request to logd.
func (p *Pool) sendRequest(conn net.Conn, req *api.SessionRequest) error {
	data, err := req.ToTony(gomap.EncodeWire(true))
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write request: %w", err)
	}
	return nil
}

// readResponse reads a response from logd.
func (p *Pool) readResponse(decoder *stream.Decoder) (*api.SessionResponse, error) {
	node, err := readDocument(decoder)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("empty response")
	}

	var resp api.SessionResponse
	if err := resp.FromTonyIR(node); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &resp, nil
}

// readDocument reads events until we have a complete document.
func readDocument(decoder *stream.Decoder) (*ir.Node, error) {
	var events []stream.Event
	started := false

	for {
		event, err := decoder.ReadEvent()
		if err != nil {
			if err == io.EOF {
				if len(events) > 0 {
					return stream.EventsToNode(events)
				}
				return nil, io.EOF
			}
			return nil, err
		}

		events = append(events, *event)
		started = true

		if started && decoder.Depth() == 0 {
			return stream.EventsToNode(events)
		}
	}
}

// Get returns a transaction ID for the given participant count.
// If no IDs are available in the pool, it fetches more from logd.
// Automatically connects if not connected.
func (p *Pool) Get(ctx context.Context, participants int) (int64, error) {
	if participants < 1 {
		return 0, fmt.Errorf("participants must be >= 1")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if closed
	select {
	case <-p.done:
		return 0, fmt.Errorf("pool closed")
	default:
	}

	// Auto-connect if needed
	if !p.connected {
		if err := p.connectLocked(ctx); err != nil {
			return 0, fmt.Errorf("failed to connect: %w", err)
		}
	}

	// Serve from the pool when possible, and refill in the background so the
	// next Get is also served without a logd round trip.
	if ids, ok := p.pools[participants]; ok && len(ids) > 0 {
		txID := ids[0]
		p.pools[participants] = ids[1:]
		p.triggerRefill(participants)
		return txID, nil
	}

	// Cache miss (cold or exhausted): fetch inline as a fallback, then warm the
	// cache for subsequent Gets.
	txID, err := p.fetchTxID(participants)
	if err != nil {
		// Connection might be broken, mark as disconnected for retry
		p.connected = false
		if p.conn != nil {
			p.conn.Close()
			p.conn = nil
		}
		return 0, fmt.Errorf("failed to fetch txID: %w", err)
	}

	p.triggerRefill(participants)
	return txID, nil
}

// triggerRefill nudges the background refiller to top up a participant count.
// Non-blocking: if a nudge is already queued, the refiller tops up to poolSize
// anyway, so dropping this one loses nothing.
func (p *Pool) triggerRefill(participants int) {
	select {
	case p.refillCh <- participants:
	default:
	}
}

// refillLoop tops up participant counts in the background as they are drained,
// so Gets keep being served from cache. This is the point of pooling: amortize
// the logd round trips instead of paying one per transaction.
func (p *Pool) refillLoop() {
	for {
		select {
		case <-p.done:
			return
		case participants := <-p.refillCh:
			p.topUp(participants)
		}
	}
}

// topUp fetches TxIDs until the participant count's pool reaches poolSize, or
// until the pool is disconnected/closed. Each fetch takes the lock only for one
// round trip, so concurrent Gets interleave rather than waiting for the whole
// batch.
func (p *Pool) topUp(participants int) {
	for {
		select {
		case <-p.done:
			return
		default:
		}

		p.mu.Lock()
		if !p.connected || len(p.pools[participants]) >= p.poolSize {
			p.mu.Unlock()
			return
		}
		txID, err := p.fetchTxID(participants)
		if err != nil {
			p.connected = false
			if p.conn != nil {
				p.conn.Close()
				p.conn = nil
			}
			p.mu.Unlock()
			return // a later Get will reconnect and re-trigger
		}
		p.pools[participants] = append(p.pools[participants], txID)
		p.mu.Unlock()
	}
}

// fetchTxID requests a new transaction ID from logd (must hold mutex).
func (p *Pool) fetchTxID(participants int) (int64, error) {
	req := &api.SessionRequest{
		NewTx: &api.NewTxRequest{
			Participants: participants,
		},
	}

	// Bound the round trip: p.mu is held here, so a hung logd must not block forever.
	if p.conn != nil {
		_ = p.conn.SetDeadline(time.Now().Add(p.ioTimeout))
		defer func() { _ = p.conn.SetDeadline(time.Time{}) }()
	}

	if err := p.sendRequest(p.conn, req); err != nil {
		return 0, err
	}

	resp, err := p.readResponse(p.decoder)
	if err != nil {
		return 0, err
	}
	if resp.Error != nil {
		return 0, fmt.Errorf("newtx error: %s", resp.Error.Message)
	}
	if resp.Result == nil || resp.Result.NewTx == nil {
		return 0, fmt.Errorf("unexpected response: no newtx result")
	}

	return resp.Result.NewTx.TxID, nil
}

// Prefetch fetches TxIDs for common participant counts in the background.
// Call this after connecting to warm up the pool.
func (p *Pool) Prefetch(ctx context.Context, participantCounts ...int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.connected {
		return
	}

	for _, n := range participantCounts {
		for i := 0; i < p.poolSize; i++ {
			select {
			case <-ctx.Done():
				return
			case <-p.done:
				return
			default:
			}

			txID, err := p.fetchTxID(n)
			if err != nil {
				p.log.Warn("prefetch failed", "participants", n, "error", err)
				return
			}
			p.pools[n] = append(p.pools[n], txID)
		}
	}

	p.log.Debug("prefetched txIDs", "counts", participantCounts, "poolSize", p.poolSize)
}

// Close shuts down the pool and closes the connection.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true
	close(p.done)

	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}

// Stats returns current pool statistics.
func (p *Pool) Stats() map[int]int {
	p.mu.Lock()
	defer p.mu.Unlock()

	stats := make(map[int]int)
	for k, v := range p.pools {
		stats[k] = len(v)
	}
	return stats
}
