package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// Server listens for incoming client connections and executes CLI commands.
type Server struct {
	Addr       string
	Token      string
	CmdFactory func() *cobra.Command
	Logger     zerolog.Logger
}

// ListenAndServe starts the TCP listener and blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", s.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()

	s.Logger.Info().Str("addr", ln.Addr().String()).Msg("daemon listening")

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
			s.Logger.Warn().Err(err).Msg("accept error")
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		s.writeError(conn, "failed to read request", 1)
		return
	}

	var req Request
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		s.writeError(conn, "invalid request JSON", 1)
		return
	}

	if req.Token != s.Token {
		s.writeError(conn, "authentication failed: invalid token", 1)
		return
	}

	s.Logger.Info().Strs("args", req.Args).Msg("handling request")

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd := s.CmdFactory()
	cmd.SetArgs(req.Args)
	cmd.SetOut(&stdoutBuf)
	cmd.SetErr(&stderrBuf)

	exitCode := 0
	if err := cmd.Execute(); err != nil {
		exitCode = 1
	}

	resp := Response{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: exitCode,
	}

	enc := json.NewEncoder(conn)
	if err := enc.Encode(resp); err != nil {
		s.Logger.Warn().Err(err).Msg("failed to write response")
	}
}

func (s *Server) writeError(conn net.Conn, msg string, code int) {
	resp := Response{Stderr: msg + "\n", ExitCode: code}
	enc := json.NewEncoder(conn)
	_ = enc.Encode(resp)
}
