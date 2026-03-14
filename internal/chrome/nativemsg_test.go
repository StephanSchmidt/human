package chrome

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadMessage_ValidMessage(t *testing.T) {
	payload := []byte(`{"text":"hello"}`)
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(payload)))
	buf.Write(payload)

	msg, err := ReadMessage(&buf)

	require.NoError(t, err)
	assert.Equal(t, payload, msg)
}

func TestReadMessage_EmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))

	msg, err := ReadMessage(&buf)

	require.NoError(t, err)
	assert.Empty(t, msg)
}

func TestReadMessage_TooLarge(t *testing.T) {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint32(MaxMessageSize+1))

	_, err := ReadMessage(&buf)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "message too large")
}

func TestReadMessage_TruncatedLength(t *testing.T) {
	buf := bytes.NewReader([]byte{0x01, 0x00}) // only 2 bytes, need 4

	_, err := ReadMessage(buf)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading message length")
}

func TestReadMessage_TruncatedBody(t *testing.T) {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint32(10))
	buf.Write([]byte("short")) // only 5 bytes, need 10

	_, err := ReadMessage(&buf)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading message body")
}

func TestReadMessage_EOF(t *testing.T) {
	buf := bytes.NewReader([]byte{})

	_, err := ReadMessage(buf)

	require.Error(t, err)
}

func TestWriteMessage_ValidPayload(t *testing.T) {
	payload := []byte(`{"text":"hello"}`)
	var buf bytes.Buffer

	err := WriteMessage(&buf, payload)

	require.NoError(t, err)

	// Verify the length prefix
	var length uint32
	require.NoError(t, binary.Read(&buf, binary.LittleEndian, &length))
	assert.Equal(t, uint32(len(payload)), length)

	// Verify the body
	body := make([]byte, length)
	_, err = io.ReadFull(&buf, body)
	require.NoError(t, err)
	assert.Equal(t, payload, body)
}

func TestWriteMessage_EmptyPayload(t *testing.T) {
	var buf bytes.Buffer

	err := WriteMessage(&buf, []byte{})

	require.NoError(t, err)

	var length uint32
	require.NoError(t, binary.Read(&buf, binary.LittleEndian, &length))
	assert.Equal(t, uint32(0), length)
}

func TestWriteMessage_TooLarge(t *testing.T) {
	data := make([]byte, MaxMessageSize+1)
	var buf bytes.Buffer

	err := WriteMessage(&buf, data)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "message too large")
}

func TestRoundTrip(t *testing.T) {
	payload := []byte(`{"id":1,"method":"ping"}`)
	var buf bytes.Buffer

	require.NoError(t, WriteMessage(&buf, payload))

	msg, err := ReadMessage(&buf)

	require.NoError(t, err)
	assert.Equal(t, payload, msg)
}

func TestRoundTrip_MultipleMessages(t *testing.T) {
	messages := [][]byte{
		[]byte(`{"id":1}`),
		[]byte(`{"id":2,"data":"test"}`),
		[]byte(`{"id":3}`),
	}

	var buf bytes.Buffer
	for _, msg := range messages {
		require.NoError(t, WriteMessage(&buf, msg))
	}

	for _, expected := range messages {
		msg, err := ReadMessage(&buf)
		require.NoError(t, err)
		assert.Equal(t, expected, msg)
	}
}
