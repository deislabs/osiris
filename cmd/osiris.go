package main

import (
	"flag"
	"strings"
	"time"

	"github.com/deislabs/osiris/pkg/signals"
	"github.com/golang/glog"
)

func main() {
	const usageMsg = `usage: must specify Osiris component to start using ` +
		`argument "activator", "endpoints-controller", "endpoints-hijacker", ` +
		`"proxy", "proxy-injector", or "zeroscaler"`

	// We need to parse flags for glog-related options to take effect
	flag.Parse()

	if len(flag.Args()) != 1 {
		glog.Fatal(usageMsg)
	}

	// This context will automatically be canceled on SIGINT or SIGTERM.
	ctx := signals.Context()

	switch strings.ToLower(flag.Arg(0)) {
	case "activator":
		runActivator(ctx)
	case "endpoints-controller":
		runEndpointsController(ctx)
	case "endpoints-hijacker":
		runEndpointsHijacker(ctx)
	case "proxy":
		runProxy(ctx)
	case "proxy-injector":
		runProxyInjector(ctx)
	case "zeroscaler":
		runZeroScaler(ctx)
	default:
		glog.Fatal(usageMsg)
	}

	// A short grace period
	shutdownDuration := 5 * time.Second
	glog.Infof("allowing %s for graceful shutdown to complete", shutdownDuration)
	<-time.After(shutdownDuration)
}
