package main

import (
	"context"

	endpoints "github.com/deislabs/osiris/pkg/endpoints/controller"
	"github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/deislabs/osiris/pkg/version"
	"github.com/golang/glog"
)

func runEndpointsController(ctx context.Context) {
	glog.Infof(
		"Starting Osiris Endpoints Controller -- version %s -- commit %s",
		version.Version(),
		version.Commit(),
	)

	client, err := kubernetes.Client()
	if err != nil {
		glog.Fatalf("Error building kubernetes clientset: %s", err)
	}

	controllerCfg, err := endpoints.GetConfigFromEnvironment()
	if err != nil {
		glog.Fatalf(
			"Error retrieving endpoints controller configuration: %s",
			err,
		)
	}

	// Run the controller
	endpoints.NewController(controllerCfg, client).Run(ctx)
}
