package main

import (
	"context"

	proxy "github.com/deislabs/osiris/pkg/metrics/proxy/injector"
	"github.com/deislabs/osiris/pkg/version"
	"github.com/golang/glog"
)

func runProxyInjector(ctx context.Context) {
	glog.Infof(
		"Starting Osiris Proxy Injector -- version %s -- commit %s",
		version.Version(),
		version.Commit(),
	)

	cfg, err := proxy.GetConfigFromEnvironment()
	if err != nil {
		glog.Fatalf(
			"Error retrieving proxy injector configuration: %s",
			err,
		)
	}

	// Run the proxy injexctor
	proxy.NewInjector(cfg).Run(ctx)
}
