package activator

import (
	"net/http/httputil"
	"net/url"
)

type app struct {
	namespace           string
	serviceName         string
	deploymentName      string
	targetURL           *url.URL
	proxyRequestHandler *httputil.ReverseProxy
}
