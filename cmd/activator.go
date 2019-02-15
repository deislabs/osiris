package main

import (
	"context"

	deployments "github.com/deislabs/osiris/pkg/deployments/activator"
	"github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/deislabs/osiris/pkg/version"
	"github.com/golang/glog"
)

func runActivator(ctx context.Context) {
	glog.Infof(
		"Starting Osiris Activator -- version %s -- commit %s",
		version.Version(),
		version.Commit(),
	)

	client, err := kubernetes.Client()
	if err != nil {
		glog.Fatalf("Error building kubernetes clientset: %s", err)
	}

	activator, err := deployments.NewActivator(client)
	if err != nil {
		glog.Fatalf("Error initializing activator: %s", err)
	}

	// Run the activator
	activator.Run(ctx)
}
