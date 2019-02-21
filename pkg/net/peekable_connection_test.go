package net

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPeekableConnection(t *testing.T) {
	var firstBytes = []byte("All your base are belong to us.")
	var secondBytes = []byte("You are on the way to destruction.")

	// Use an in-memory connection pair
	remoteConn, localConn := net.Pipe()
	defer remoteConn.Close()
	peekableConn := NewPeekableConn(localConn)
	go func() {
		n, err := remoteConn.Write(firstBytes)
		require.NoError(t, err)
		require.Equal(t, len(firstBytes), n)
	}()

	// Peek at nothing. This is pointless, but shouldn't fail.
	bytes, err := peekableConn.Peek(0)
	require.NoError(t, err)
	require.Zero(t, len(bytes))

	// A very small peek
	bytes, err = peekableConn.Peek(1)
	require.NoError(t, err)
	require.Equal(t, firstBytes[:1], bytes)

	// A somewhat bigger peek
	bytes, err = peekableConn.Peek(5)
	require.NoError(t, err)
	require.Equal(t, firstBytes[:5], bytes)

	// Peek at the whole buffer
	bytes, err = peekableConn.Peek(len(firstBytes))
	require.NoError(t, err)
	require.Equal(t, firstBytes, bytes)

	// Try to peek past the end of the buffer
	bytes, err = peekableConn.Peek(len(firstBytes) + 1)
	require.NoError(t, err)
	require.Equal(t, firstBytes, bytes)

	// Try a read
	bytes = make([]byte, 1024)
	n, err := peekableConn.Read(bytes)
	require.NoError(t, err)
	require.Equal(t, firstBytes, bytes[:n])

	// Try peeking again. Peeking after a read should be an error.
	bytes, err = peekableConn.Peek(1)
	require.Error(t, err)
	require.Nil(t, bytes)
	require.Contains(t, err.Error(), "Cannot peek")

	// Write some more stuff so we can test reading past the buffer
	go func() {
		m, gerr := remoteConn.Write(secondBytes)
		require.NoError(t, gerr)
		require.Equal(t, len(secondBytes), m)
	}()

	bytes = make([]byte, 1024)
	n, err = peekableConn.Read(bytes)
	require.NoError(t, err)
	require.Equal(t, secondBytes, bytes[:n])

	// Make sure we can close the connection cleanly
	require.NoError(t, peekableConn.Close())
}
