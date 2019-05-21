package http

import (
	"io"
	"net"
	"sync"
)

// singleConnectionListener implements the net.Listener interface and wraps an
// established net.Conn. The purpose of this type is to return the established
// connection once and only once upon invoation of this type's Accept()
// function. This type is essentially an adapter that permits us to create a
// bespoke server for handling a single, established connection, despite the
// fact that an HTTP server in Go typically takes a net.Listener as an argument
// to its Serve() function rather than a net.Conn.
type singleConnectionListener struct {
	conn net.Conn
	once sync.Once
}

// Accept will return the singleConnectionListener's established net.Conn once
// and only once. Subsequent calls will return an io.EOF error.
func (s *singleConnectionListener) Accept() (net.Conn, error) {
	var c net.Conn
	s.once.Do(func() {
		c = s.conn
	})
	if c != nil {
		return c, nil
	}
	return nil, io.EOF
}

// Close is a no-op. Since a singleConnectionListener is not responsible for
// opening connections, it is also not responsible for closing them.
func (s *singleConnectionListener) Close() error {
	return nil
}

func (s *singleConnectionListener) Addr() net.Addr {
	return s.conn.LocalAddr()
}
