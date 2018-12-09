package metrics

type ProxyRequestCount struct {
	ProxyID      string `json:"proxyId"`
	RequestCount uint64 `json:"requestCount"`
}
