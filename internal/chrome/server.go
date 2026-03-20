package chrome

import (
	"bufio"
	"context"
	"encoding/json"
	"net"

	"github.com/rs/zerolog"

	"github.com/StephanSchmidt/human/errors"
)

// Server listens for chrome-proxy connections on its own TCP port.
type Server struct {
	Addr       string
	Token      string
	Translator *McpTranslator
	Logger     zerolog.Logger
}

// ListenAndServe starts the TCP listener and blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", s.Addr)
	if err != nil {
		return errors.WrapWithDetails(err, "chrome proxy listen failed",
			"addr", s.Addr)
	}
	defer func() { _ = ln.Close() }()

	s.Logger.Info().Str("addr", ln.Addr().String()).Msg("chrome proxy listening")

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.Logger.Warn().Err(err).Msg("chrome proxy accept error")
			continue
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	// Read the auth request (single JSON line).
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		s.writeAck(conn, false, "failed to read request")
		return
	}

	var req proxyRequest
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		s.writeAck(conn, false, "invalid request JSON")
		return
	}

	if req.Token != s.Token {
		s.writeAck(conn, false, "authentication failed: invalid token")
		return
	}

	s.Logger.Info().Msg("starting chrome-proxy session")

	s.writeAck(conn, true, "")

	if err := s.Translator.Serve(ctx, conn); err != nil {
		s.Logger.Warn().Err(err).Msg("chrome proxy error")
	}
}

func (s *Server) writeAck(conn net.Conn, ok bool, errMsg string) {
	ack := ProxyAck{OK: ok, Error: errMsg}
	enc := json.NewEncoder(conn)
	_ = enc.Encode(ack)
}
