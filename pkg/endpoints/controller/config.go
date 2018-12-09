package controller

import "github.com/kelseyhightower/envconfig"

const envconfigPrefix = "OSIRIS_ENDPOINTS_CONTROLLER"

// Config represents configuration options for the Osiris endpoints controller
// nolint: lll
type Config struct {
	OsirisNamespace                string `envconfig:"OSIRIS_NAMESPACE" required:"true"`
	ActivatorPodLabelSelectorKey   string `envconfig:"ACTIVATOR_POD_LABEL_SELECTOR_KEY" required:"true"`
	ActivatorPodLabelSelectorValue string `envconfig:"ACTIVATOR_POD_LABEL_SELECTOR_VALUE" required:"true"`
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
