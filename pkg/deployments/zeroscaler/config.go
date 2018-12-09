package zeroscaler

import "github.com/kelseyhightower/envconfig"

const envconfigPrefix = "ZEROSCALER"

// Config represents the configuration options for zeroscaler
// nolint: lll
type Config struct {
	MetricsCheckInterval int `envconfig:"METRICS_CHECK_INTERVAL" required:"true"`
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
