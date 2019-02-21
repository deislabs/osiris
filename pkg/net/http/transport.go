package http

import (
	"crypto/tls"
	"net"
	"net/http"

	"golang.org/x/net/http2"
)

// h2cDefaultTransport is transport for HTTP/2 WITHOUT TLS.
var h2cDefaultTransport http.RoundTripper = &http2.Transport{
	AllowHTTP: true,
	DialTLS: func(netw, addr string, cfg *tls.Config) (net.Conn, error) {
		return net.Dial(netw, addr)
	},
}
