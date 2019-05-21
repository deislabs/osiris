package http

import (
	"regexp"

	"github.com/deislabs/osiris/pkg/net"
)

var httpReqRegex = regexp.MustCompile(`\A[A-Z]+\s+\S+\s+HTTP/((?:1\.0)|(?:1\.1)|(?:2\.0))\r?\n`) // nolint: lll

// Version peeks at a PeekableConn and attempts to determine whether it is being
// used for HTTP requests. If so, and it can recognize the HTTP version, the
// version is returned. In all other cases, an empty string is returned.
func Version(conn net.PeekableConn) string {
	// The underlying buffer holds 4096 bytes and every time we peek, the buffer
	// tries to completely fill itself, so we may as well jump to peeking at as
	// many bytes as the buffer can contain. It makes more sense than iteratively
	// taking larger and larger peeks until we've found something we can make
	// sense of.
	const maxPeekSize = 4096
	peekBytes, _ := conn.Peek(maxPeekSize)
	matches := httpReqRegex.FindStringSubmatch(string(peekBytes))
	if len(matches) == 2 {
		return matches[1]
	}
	return ""
}
