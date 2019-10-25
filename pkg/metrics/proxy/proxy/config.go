package proxy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/kelseyhightower/envconfig"
)

const envconfigPrefix = "OSIRIS_PROXY"

var portMappingRegex = regexp.MustCompile(`^(?:\d+\:\d+,)*(?:\d+\:\d+)$`)

// config is a package-internal representation of configuration options for the
// Osiris Proxy
type config struct {
	PortMappings         string `envconfig:"PORT_MAPPINGS" required:"true"`
	MetricsAndHealthPort int    `envconfig:"METRICS_AND_HEALTH_PORT" required:"true"` // nolint: lll
	// comma-separated list of URL paths that won't be counted
	IgnoredPaths string `envconfig:"IGNORED_PATHS"`
}

// Config represents configuration options for the Osiris Proxy
type Config struct {
	PortMappings         map[int]int
	MetricsAndHealthPort int
	IgnoredPaths         map[string]struct{}
}

// NewConfigWithDefaults returns a Config object with default values already
// applied. Callers are then free to set custom values for the remaining fields
// and/or override default values.
func NewConfigWithDefaults() Config {
	return Config{
		PortMappings: map[int]int{},
	}
}

// GetConfigFromEnvironment returns configuration derived from environment
// variables
func GetConfigFromEnvironment() (Config, error) {
	c := NewConfigWithDefaults()

	internalC := config{}
	if err := envconfig.Process(envconfigPrefix, &internalC); err != nil {
		return c, err
	}

	if !portMappingRegex.MatchString(internalC.PortMappings) {
		return c, fmt.Errorf("Invalid port mappings: %s", internalC.PortMappings)
	}

	mappingStrs := strings.Split(internalC.PortMappings, ",")
	for _, mappingStr := range mappingStrs {
		mappingTokens := strings.Split(mappingStr, ":")
		proxyPort, _ := strconv.Atoi(mappingTokens[0])
		appPort, _ := strconv.Atoi(mappingTokens[1])
		c.PortMappings[proxyPort] = appPort
	}

	c.MetricsAndHealthPort = internalC.MetricsAndHealthPort

	ignoredPaths := strings.Split(internalC.IgnoredPaths, ",")
	if len(ignoredPaths) > 0 {
		c.IgnoredPaths = make(map[string]struct{}, len(ignoredPaths))
		for _, ignoredPath := range ignoredPaths {
			c.IgnoredPaths[ignoredPath] = struct{}{}
		}
	}

	return c, nil
}
