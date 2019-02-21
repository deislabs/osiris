package tcp

import (
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/golang/glog"
)

type l4StartProxyCallback func(serverName string) (string, int, error)
type l4EndProxyCallback func(serverName string) error

func defaultProxyConnection(
	conn net.Conn,
	serverName string,
	startProxyCallback l4StartProxyCallback,
	endProxyCallback l4EndProxyCallback,
) error {
	targetServerName := serverName
	targetPort := 443

	if endProxyCallback != nil {
		defer func() {
			if err := endProxyCallback(serverName); err != nil {
				glog.Errorf(
					"Error executing end proxy callback for server name \"%s\": %s",
					serverName,
					err,
				)
			}
		}()
	}

	if startProxyCallback != nil {
		var err error
		if targetServerName, targetPort, err =
			startProxyCallback(serverName); err != nil {
			return fmt.Errorf(
				"Error executing start proxy callback for server name \"%s\": %s",
				serverName,
				err,
			)
		}
	}
	targetAddr, err := net.ResolveTCPAddr(
		"tcp",
		fmt.Sprintf("%s:%d", targetServerName, targetPort),
	)
	if err != nil {
		return fmt.Errorf(
			"Error resolving target address %s:%d",
			serverName,
			targetPort,
		)
	}
	targetConn, err := net.DialTCP("tcp", nil, targetAddr)
	if err != nil {
		return fmt.Errorf("Error dialing target address %s", targetAddr.String())
	}
	defer targetConn.Close()
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_, gerr := io.Copy(conn, targetConn)
		// Connections reset by peer isn't really a problem. It just means the
		// destination end hung up already.
		if gerr != nil &&
			!strings.Contains(gerr.Error(), "connection reset by peer") {
			glog.Errorf(
				"Error streaming bytes from target connection to client connection: %s",
				gerr,
			)
		}
	}()
	_, err = io.Copy(targetConn, conn)
	// Connections reset by peer isn't really a problem. It just means the
	// destination end hung up already.
	if err != nil &&
		!strings.Contains(err.Error(), "connection reset by peer") {
		glog.Errorf(
			"Error streaming bytes from client connection to target connection: %s",
			err,
		)
	}
	<-doneCh // Wait until copy in BOTH directions is done
	return nil
}
