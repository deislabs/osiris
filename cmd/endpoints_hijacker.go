package main

import (
	"context"

	endpoints "github.com/deislabs/osiris/pkg/endpoints/hijacker"
	"github.com/deislabs/osiris/pkg/version"
	"github.com/golang/glog"
)

func runEndpointsHijacker(ctx context.Context) {
	glog.Infof(
		"Starting Osiris Endpoints Hijacker -- version %s -- commit %s",
		version.Version(),
		version.Commit(),
	)

	cfg, err := endpoints.GetConfigFromEnvironment()
	if err != nil {
		glog.Fatalf(
			"Error retrieving proxy endpoints hijacker webhook server "+
				"configuration: %s",
			err,
		)
	}

	// Run the server
	endpoints.NewHijacker(cfg).Run(ctx)
}
