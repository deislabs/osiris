package net

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"sync"
)

// PeekableConn is an interface that is a superset of the net.Conn interface,
// additionally allowing a connection's bytes to be inspected without being
// consumed.
type PeekableConn interface {
	net.Conn
	// Peek returns a preview of n bytes of the connection without consuming them.
	// Once this connection has been read from, peeking is no longer permitted and
	// will return an error.
	Peek(n int) ([]byte, error)
}

// peekableConn implements PeekableConn by wrapping a net.Conn and providing
// functionality for inspecting bytes from that connection without consuming
// them.
type peekableConn struct {
	peekedBytes []byte
	br          *bufio.Reader
	net.Conn
	mu   sync.Mutex
	once sync.Once
}

// NewPeekableConn returns a PeekableConn whose bytes can be inspected without
// being consumed.
func NewPeekableConn(conn net.Conn) PeekableConn {
	return &peekableConn{
		br:   bufio.NewReader(conn),
		Conn: conn,
	}
}

func (p *peekableConn) Peek(n int) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.peekedBytes != nil {
		return nil,
			errors.New("Cannot peek at a connection that has been read from")
	}
	// This will actually fill the buffer with as much as is available from the
	// underlying io.Reader
	_, err := p.br.Peek(1)
	if err != nil {
		return nil, fmt.Errorf("Error peeking at connection: %s", err)
	}
	bufferedBytesCount := p.br.Buffered()
	// If more bytes were requested than are available in the buffer, return no
	// more than what is available, otherwise, we'll block waiting for more bytes
	if bufferedBytesCount < n {
		n = bufferedBytesCount
	}
	return p.br.Peek(n)
}

func (p *peekableConn) Read(bytes []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.once.Do(
		func() {
			p.peekedBytes, _ = p.br.Peek(p.br.Buffered())
		},
	)
	if len(p.peekedBytes) > 0 {
		n := copy(bytes, p.peekedBytes)
		p.peekedBytes = p.peekedBytes[n:]
		return n, nil
	}
	return p.Conn.Read(bytes)
}
