package injector

import "github.com/kelseyhightower/envconfig"

const envconfigPrefix = "OSIRIS_PROXY_INJECTOR"

// Config represents configuration options for the Osiris Proxy Injector
// webhook server
type Config struct {
	TLSCertFile          string `envconfig:"TLS_CERT_FILE" required:"true"`
	TLSKeyFile           string `envconfig:"TLS_KEY_FILE" required:"true"`
	ProxyImage           string `envconfig:"PROXY_IMAGE" required:"true"`
	ProxyImagePullPolicy string `envconfig:"PROXY_IMAGE_PULL_POLICY"`
	// comma-separated list of URL paths that won't be counted
	IgnoredPaths string `envconfig:"IGNORED_PATHS"`
}

// NewConfigWithDefaults returns a Config object with default values already
// applied. Callers are then free to set custom values for the remaining fields
// and/or override default values.
func NewConfigWithDefaults() Config {
	return Config{
		ProxyImagePullPolicy: "IfNotPresent",
	}
}

// GetConfigFromEnvironment returns configuration derived from environment
// variables
func GetConfigFromEnvironment() (Config, error) {
	c := NewConfigWithDefaults()
	err := envconfig.Process(envconfigPrefix, &c)
	return c, err
}
