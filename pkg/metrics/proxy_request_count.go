package metrics

type ProxyConnectionStats struct {
	ProxyID           string `json:"proxyId"`
	ConnectionsOpened uint64 `json:"connectionsOpened"`
	ConnectionsClosed uint64 `json:"connectionsClosed"`
}
