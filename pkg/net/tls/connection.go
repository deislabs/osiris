package tls

import (
	"bytes"
	"crypto/tls"
	"io"
	"net"

	mynet "github.com/deislabs/osiris/pkg/net"
)

type sniSniffConn struct {
	r        io.Reader
	net.Conn // nil; crash on any unexpected use
}

func (c sniSniffConn) Read(p []byte) (int, error) { return c.r.Read(p) }
func (sniSniffConn) Write(p []byte) (int, error)  { return 0, io.EOF }

// ClientHelloServerName partially completes a TLS handshake to learn the
// target serverName as specified by SNI.
func ClientHelloServerName(conn mynet.PeekableConn) string {
	const recordHeaderLen = 5
	hdr, err := conn.Peek(recordHeaderLen)
	if err != nil {
		return ""
	}
	const recordTypeHandshake = 0x16
	if hdr[0] != recordTypeHandshake {
		return "" // Not TLS.
	}
	recLen := int(hdr[3])<<8 | int(hdr[4]) // ignoring version in hdr[1:3]
	helloBytes, err := conn.Peek(recordHeaderLen + recLen)
	if err != nil {
		return ""
	}
	var serverName string
	if err := tls.Server(
		sniSniffConn{r: bytes.NewReader(helloBytes)},
		&tls.Config{
			GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) { // nolint: lll
				serverName = hello.ServerName
				return nil, nil
			},
		},
	).Handshake(); err != nil {
		// We don't actually expect this to succeed. We only needed to get far
		// enough to sniff the SNI server name. Don't even bother to log this.
	}
	return serverName
}
