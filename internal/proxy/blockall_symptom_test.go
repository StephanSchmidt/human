package proxy

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What a block-all policy looks like from inside a container, recorded because
// the symptom pointed everyone at the wrong thing for a week (SC-4819).
//
// The server closes the connection right after reading the ClientHello, so a
// real TLS client sees an EOF mid-handshake and reports it as a certificate
// error — Claude Code's UNKNOWN_CERTIFICATE_VERIFICATION_ERROR. No certificate
// is involved: the proxy never terminates TLS on this path and presents none.
func TestServer_blockAllKillsTheHandshakeBeforeAnyCertificate(t *testing.T) {
	srv := &Server{Policy: BlockAllPolicy(), Logger: zerolog.Nop()}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv.Addr = ln.Addr().String()
	_ = ln.Close()

	go func() { _ = srv.ListenAndServe(t.Context()) }()
	time.Sleep(50 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", srv.Addr, time.Second)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))

	client := tls.Client(conn, &tls.Config{ServerName: DefaultModelAPIHost, MinVersion: tls.VersionTLS12})
	err = client.HandshakeContext(t.Context())

	require.Error(t, err, "block-all must not complete a handshake")
	var certErr *tls.CertificateVerificationError
	assert.False(t, errors.As(err, &certErr),
		"the client blames the certificate, but no certificate was ever sent: %v", err)
	assert.True(t, errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF),
		"the handshake dies on EOF, which is what the container reports as a certificate failure: %v", err)
}

// BlockAllPolicy is an allowlist with no matchers, so it rejects the model API
// exactly as it rejects everything else — the state the daemon silently fell
// into whenever it found no proxy config.
func TestBlockAllPolicy_rejectsTheModelAPI(t *testing.T) {
	assert.False(t, BlockAllPolicy().Allowed(DefaultModelAPIHost))
	assert.False(t, BlockAllPolicy().Allowed("github.com"))
}
