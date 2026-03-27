package proxy

import (
	"context"
	"net"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"

	"github.com/StephanSchmidt/human/errors"
)

// Server is a transparent HTTPS proxy that reads the SNI from TLS ClientHello
// to block/allow domains without decrypting traffic. Domains listed in the
// Interceptor are MITM'd for traffic inspection/logging.
type Server struct {
	Addr        string
	Policy      Decider
	Interceptor Interceptor // optional: MITM interceptor for specific domains
	Logger      zerolog.Logger
	// Dialer connects to upstream servers. Injected for testing.
	Dialer func(ctx context.Context, network, address string) (net.Conn, error)

	activeConns atomic.Int64 // number of currently-active forwarded connections
}

// ActiveConns returns the number of currently active forwarded connections.
func (s *Server) ActiveConns() int64 {
	return s.activeConns.Load()
}

// ListenAndServe starts the TCP listener and blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", s.Addr)
	if err != nil {
		return errors.WrapWithDetails(err, "https proxy listen failed",
			"addr", s.Addr)
	}
	closeOnce := sync.Once{}
	closeLn := func() { closeOnce.Do(func() { _ = ln.Close() }) }
	defer closeLn()

	s.Logger.Info().Str("addr", ln.Addr().String()).Msg("https proxy listening")

	go func() {
		<-ctx.Done()
		closeLn()
	}()

	for {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.Logger.Warn().Err(acceptErr).Msg("https proxy accept error")
			continue
		}
		if conn == nil {
			continue
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	peeked, serverName, err := PeekClientHello(conn)
	if err != nil {
		s.Logger.Debug().Err(err).Msg("SNI extraction failed")
		return
	}

	if serverName == "" {
		s.Logger.Debug().Msg("no SNI in ClientHello, blocking")
		return
	}

	if !s.Policy.Allowed(serverName) {
		s.Logger.Info().Str("host", serverName).Msg("blocked by policy")
		return
	}

	// Check if this domain should be intercepted (MITM for logging/inspection).
	if s.Interceptor != nil && s.Interceptor.ShouldIntercept(serverName) {
		s.activeConns.Add(1)
		defer s.activeConns.Add(-1)

		s.Logger.Info().Str("host", serverName).Msg("intercepting (MITM)")
		if interceptErr := s.Interceptor.Intercept(ctx, conn, serverName, peeked); interceptErr != nil {
			s.Logger.Warn().Err(interceptErr).Str("host", serverName).Msg("intercept failed")
		}
		return
	}

	dialer := s.dialer()
	upstream, err := dialer(ctx, "tcp", net.JoinHostPort(serverName, "443"))
	if err != nil {
		s.Logger.Warn().Err(err).Str("host", serverName).Msg("upstream dial failed")
		return
	}

	s.activeConns.Add(1)
	defer s.activeConns.Add(-1)

	s.Logger.Info().Str("host", serverName).Msg("forwarding")
	Forward(ctx, conn, upstream, peeked, s.Logger)
}

func (s *Server) dialer() func(ctx context.Context, network, address string) (net.Conn, error) {
	if s.Dialer != nil {
		return s.Dialer
	}
	d := &net.Dialer{}
	return d.DialContext
}
