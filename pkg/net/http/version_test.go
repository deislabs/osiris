package http

import (
	"net"
	"testing"

	mynet "github.com/deislabs/osiris/pkg/net"
	"github.com/stretchr/testify/require"
)

func TestVersion(t *testing.T) {
	testCases := []struct {
		name        string
		bytes       []byte
		httpVersion string
	}{
		{
			name:        "HTTP 1.0 GET request",
			bytes:       []byte("GET /foo HTTP/1.0\nHost: foo.com"),
			httpVersion: "1.0",
		},
		{
			name:        "HTTP 1.1 GET request",
			bytes:       []byte("GET /foo HTTP/1.1\nHost: foo.com"),
			httpVersion: "1.1",
		},
		{
			name:        "HTTP 1.1 POST request",
			bytes:       []byte("POST /foo HTTP/1.1\nHost: foo.com"),
			httpVersion: "1.1",
		},
		{
			name:        "HTTP 2.0 PRE request",
			bytes:       []byte("PRE * HTTP/2.0\nHost: foo.com"),
			httpVersion: "2.0",
		},
		{
			name:        "HTTP 1.1 GET request with carriage return",
			bytes:       []byte("GET /foo HTTP/1.1\r\nHost: foo.com"),
			httpVersion: "1.1",
		},
		{
			name:        "gibberish",
			bytes:       []byte("All your base are belong to us."),
			httpVersion: "",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Use an in-memory connection pair
			remoteConn, localConn := net.Pipe()
			defer remoteConn.Close()
			defer localConn.Close()
			peekableConn := mynet.NewPeekableConn(localConn)
			// Use a local variable so that our goroutine doesn't close over something
			// that will change as we iterate
			bytes := testCase.bytes
			go func() {
				n, err := remoteConn.Write(bytes)
				require.NoError(t, err)
				require.Equal(t, len(bytes), n)
			}()
			require.Equal(
				t,
				testCase.httpVersion,
				Version(peekableConn),
			)
		})
	}
}
