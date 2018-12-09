package hijacker

import "github.com/kelseyhightower/envconfig"

const envconfigPrefix = "OSIRIS_ENDPOINTS_HIJACKER"

// Config represents configuration options for the Osiris Proxy Injecgtor
// webhook server
type Config struct {
	TLSCertFile string `envconfig:"TLS_CERT_FILE" required:"true"`
	TLSKeyFile  string `envconfig:"TLS_KEY_FILE" required:"true"`
}

// NewConfigWithDefaults returns a Config object with default values already
// applied. Callers are then free to set custom values for the remaining fields
// and/or override default values.
func NewConfigWithDefaults() Config {
	return Config{}
}

// GetConfigFromEnvironment returns configuration derived from environment
// variables
func GetConfigFromEnvironment() (Config, error) {
	c := NewConfigWithDefaults()
	err := envconfig.Process(envconfigPrefix, &c)
	return c, err
}
