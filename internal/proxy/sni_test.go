package proxy

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildClientHello constructs a minimal TLS ClientHello with the given SNI.
func buildClientHello(serverName string) []byte {
	// Build SNI extension data.
	var sniExt []byte
	if serverName != "" {
		nameBytes := []byte(serverName)
		nameLen := len(nameBytes)
		// server_name list entry: type(1) + length(2) + name
		entry := []byte{sniHostNameType, byte(nameLen >> 8), byte(nameLen)}
		entry = append(entry, nameBytes...)
		// server_name list: length(2) + entries
		listLen := len(entry)
		sniList := []byte{byte(listLen >> 8), byte(listLen)}
		sniList = append(sniList, entry...)
		// extension header: type(2) + length(2) + data
		extData := sniList
		sniExt = []byte{0x00, 0x00, byte(len(extData) >> 8), byte(len(extData))}
		sniExt = append(sniExt, extData...)
	}

	// Extensions block: length(2) + extensions
	var extensions []byte
	if len(sniExt) > 0 {
		extLen := len(sniExt)
		extensions = []byte{byte(extLen >> 8), byte(extLen)}
		extensions = append(extensions, sniExt...)
	}

	// ClientHello body:
	// version(2) + random(32) + sessionID len(1) + ciphers len(2) + one cipher(2) + comp len(1) + comp(1)
	hello := make([]byte, 0, 256)
	hello = append(hello, 0x03, 0x03)             // client version TLS 1.2
	hello = append(hello, make([]byte, 32)...)    // random
	hello = append(hello, 0x00)                   // session ID length = 0
	hello = append(hello, 0x00, 0x02, 0x00, 0x2f) // cipher suites: length=2, TLS_RSA_WITH_AES_128_CBC_SHA
	hello = append(hello, 0x01, 0x00)             // compression: length=1, null
	hello = append(hello, extensions...)

	// Handshake message: type(1) + length(3) + body
	helloLen := len(hello)
	handshake := []byte{handshakeClientHello, 0x00, byte(helloLen >> 8), byte(helloLen)}
	handshake = append(handshake, hello...)

	// TLS record: type(1) + version(2) + length(2) + body
	recordLen := len(handshake)
	record := []byte{tlsRecordHandshake, 0x03, 0x01, byte(recordLen >> 8), byte(recordLen)}
	record = append(record, handshake...)

	return record
}

func TestPeekClientHello_validSNI(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	hello := buildClientHello("github.com")

	go func() {
		_, _ = client.Write(hello)
	}()

	peeked, name, err := PeekClientHello(server)

	require.NoError(t, err)
	assert.Equal(t, "github.com", name)
	assert.Equal(t, hello, peeked)
}

func TestPeekClientHello_noSNI(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	hello := buildClientHello("")

	go func() {
		_, _ = client.Write(hello)
	}()

	peeked, name, err := PeekClientHello(server)

	require.NoError(t, err)
	assert.Empty(t, name)
	assert.Equal(t, hello, peeked)
}

func TestPeekClientHello_nonTLSInput(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	go func() {
		_, _ = client.Write([]byte("GET / HTTP/1.1\r\n"))
	}()

	_, _, err := PeekClientHello(server)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a TLS handshake record")
}

func TestPeekClientHello_truncatedHeader(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = server.Close() }()

	go func() {
		_, _ = client.Write([]byte{0x16, 0x03})
		_ = client.Close()
	}()

	_, _, err := PeekClientHello(server)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading TLS record header")
}

func TestPeekClientHello_truncatedBody(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = server.Close() }()

	// Valid header claiming 100 bytes, but only send 10.
	header := []byte{tlsRecordHandshake, 0x03, 0x01, 0x00, 100}

	go func() {
		_, _ = client.Write(header)
		_, _ = client.Write(make([]byte, 10))
		_ = client.Close()
	}()

	_, _, err := PeekClientHello(server)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading TLS record body")
}

func TestPeekClientHello_longServerName(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	hello := buildClientHello("very-long-subdomain.example.github.com")

	go func() {
		_, _ = client.Write(hello)
	}()

	peeked, name, err := PeekClientHello(server)

	require.NoError(t, err)
	assert.Equal(t, "very-long-subdomain.example.github.com", name)
	assert.Equal(t, hello, peeked)
}
