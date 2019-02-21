package tcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	mynet "github.com/deislabs/osiris/pkg/net"
	"github.com/deislabs/osiris/pkg/net/http"
	"github.com/deislabs/osiris/pkg/net/tls"
	"github.com/golang/glog"
)

// DynamicProxy is an interface for components that can listen for TCP
// connections and, after accepting them, dynamically determine whether an
// L7 proxy (HTTP) or L4 proxy (plain TCP, with an assumption of TLS) is most
// appropriate. Implementations of this interface will delegate all further
// connection handling to the most appropriate proxy type.
type DynamicProxy interface {
	ListenAndServe(ctx context.Context) error
}

type dynamicProxy struct {
	listenAddr *net.TCPAddr
	// This can be overridden for testing purposes
	serveConnectionFn func(net.Conn) error
	// This can be overridden for testing purposes
	httpVersionFn func(conn mynet.PeekableConn) string
	// This can be overridden for testing purposes
	clientHelloServerNameFn func(
		conn mynet.PeekableConn,
	) (sni string)
	l7StartProxyCallback http.L7StartProxyCallback
	// This can be overridden for testing purposes
	l7ProxyFn func(
		conn net.Conn,
		httpVersion string,
		startProxyCallback http.L7StartProxyCallback,
		endProxyCallback http.L7EndProxyCallback,
	) error
	l7EndProxyCallback   http.L7EndProxyCallback
	l4StartProxyCallback l4StartProxyCallback
	// This can be overridden for testing purposes
	l4ProxyFn func(
		conn net.Conn,
		serverAddr string,
		startProxyCallback l4StartProxyCallback,
		endProxyCallback l4EndProxyCallback,
	) error
	l4EndProxyCallback l4EndProxyCallback
}

// NewDynamicProxy returns a DynamicProxy.
func NewDynamicProxy(
	listenAddrStr string,
	l7StartProxyCallback http.L7StartProxyCallback,
	l7EndProxyCallback http.L7EndProxyCallback,
	l4StartProxyCallback l4StartProxyCallback,
	l4EndProxyCallback l4EndProxyCallback,
) (DynamicProxy, error) {
	listenAddr, err := net.ResolveTCPAddr("tcp", listenAddrStr)
	if err != nil {
		return nil, fmt.Errorf(
			`Error resolving listen address "%s": %s`,
			listenAddrStr,
			err,
		)
	}
	d := &dynamicProxy{
		listenAddr:              listenAddr,
		httpVersionFn:           http.Version,
		clientHelloServerNameFn: tls.ClientHelloServerName,
		l7StartProxyCallback:    l7StartProxyCallback,
		l7ProxyFn:               http.ProxySingleConnection,
		l7EndProxyCallback:      l7EndProxyCallback,
		l4StartProxyCallback:    l4StartProxyCallback,
		l4ProxyFn:               defaultProxyConnection,
		l4EndProxyCallback:      l4EndProxyCallback,
	}
	d.serveConnectionFn = d.defaultServeConnection
	return d, nil
}

func (d *dynamicProxy) ListenAndServe(ctx context.Context) error {
	listener, err := net.ListenTCP("tcp", d.listenAddr)
	if err != nil {
		return fmt.Errorf(
			`Error creating listener for local address "%s": %s`,
			d.listenAddr,
			err,
		)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// Only spend five seconds at a time listening for connections. This gives
			// us an opportunity to loop and check if the context has expired.
			if err :=
				listener.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				glog.Errorf("Error setting read deadline: %s", err)
			}
			conn, err := listener.AcceptTCP()
			if err != nil {
				if err, ok := err.(net.Error); !ok || !err.Timeout() {
					glog.Errorf("Error accepting connection: %s", err)
				}
				continue
			}
			go func() {
				defer conn.Close()
				if err := d.serveConnectionFn(conn); err != nil {
					glog.Errorf("Error serving connection: %s", err)
				}
			}()
		}
	}
}

func (d *dynamicProxy) defaultServeConnection(conn net.Conn) error {
	peekableConn := mynet.NewPeekableConn(conn)
	httpVersion := d.httpVersionFn(peekableConn)
	if httpVersion != "" {
		if err := d.l7ProxyFn(
			peekableConn,
			httpVersion,
			d.l7StartProxyCallback,
			d.l7EndProxyCallback,
		); err != nil {
			return fmt.Errorf("Error applying l7 proxy: %s", err)
		}
		return nil
	}
	serverName := d.clientHelloServerNameFn(peekableConn)
	if serverName == "" {
		return errors.New("Connection not recognized as being used for HTTP or TLS")
	}
	if err := d.l4ProxyFn(
		peekableConn,
		serverName,
		d.l4StartProxyCallback,
		d.l4EndProxyCallback,
	); err != nil {
		return fmt.Errorf("Error applying l4 proxy: %s", err)
	}
	return nil
}
