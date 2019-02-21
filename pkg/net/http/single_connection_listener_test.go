package http

import (
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSingleConnectionListener(t *testing.T) {
	// Use an in-memory connection pair
	remoteConn, localConn := net.Pipe()
	defer remoteConn.Close()
	l := &singleConnectionListener{
		conn: localConn,
	}
	require.Equal(t, localConn.LocalAddr(), l.Addr())
	conn, err := l.Accept()
	require.NoError(t, err)
	require.Equal(t, localConn, conn)
	conn, err = l.Accept()
	require.Error(t, err)
	require.Equal(t, io.EOF, err)
	require.Nil(t, conn)
	require.NoError(t, l.Close())
}
