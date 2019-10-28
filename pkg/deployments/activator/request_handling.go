package activator

import (
	"fmt"

	"github.com/golang/glog"
)

func (a *activator) activateAndWait(hostname string) (string, int, error) {
	glog.Infof("Request received for host %s", hostname)

	a.indicesLock.RLock()
	app, ok := a.appsByHost[hostname]
	a.indicesLock.RUnlock()
	if !ok {
		return "", 0, fmt.Errorf(
			"No deployment or statefulSet found for host %s",
			hostname,
		)
	}

	glog.Infof(
		"%s %s in namespace %s may require activation",
		app.kind,
		app.name,
		app.namespace,
	)

	// Are we already activating the deployment/statefulset in question?
	var err error
	appKey := getKey(app.namespace, app.kind, app.name)
	appActivation, ok := a.appActivations[appKey]
	if ok {
		glog.Infof(
			"Found activation in-progress for %s %s in namespace %s",
			app.kind,
			app.name,
			app.namespace,
		)
	} else {
		func() {
			a.appActivationsLock.Lock()
			defer a.appActivationsLock.Unlock()
			// Some other goroutine could have initiated activation of this
			// deployment/statefulSet while we were waiting for the lock.
			// Now that we have the lock, do we still need to do this?
			appActivation, ok = a.appActivations[appKey]
			if ok {
				glog.Infof(
					"Found activation in-progress for %s %s in namespace %s",
					app.kind,
					app.name,
					app.namespace,
				)
				return
			}
			glog.Infof(
				"Found NO activation in-progress for %s %s in namespace %s",
				app.kind,
				app.name,
				app.namespace,
			)
			// Initiate activation (or discover that it may already have been started
			// by another activator process)
			switch app.kind {
			case appKindDeployment:
				appActivation, err = a.activateDeployment(app)
			case appKindStatefulSet:
				appActivation, err = a.activateStatefulSet(app)
			default:
				glog.Errorf("unknown app kind %s", app.kind)
				return
			}
			if err != nil {
				glog.Errorf(
					"%s activation for %s in namespace %s failed: %s",
					app.kind,
					app.name,
					app.namespace,
					err,
				)
				return
			}
			// Add it to the index of in-flight activation
			a.appActivations[appKey] = appActivation
			// But remove it from that index when it's complete
			go func() {
				deleteActivation := func() {
					a.appActivationsLock.Lock()
					defer a.appActivationsLock.Unlock()
					delete(a.appActivations, appKey)
				}
				select {
				case <-appActivation.successCh:
					deleteActivation()
				case <-appActivation.timeoutCh:
					deleteActivation()
				}
			}()
		}()
		if err != nil {
			return "", 0, fmt.Errorf(
				"Error activating %s %s in namespace %s: %s",
				app.kind,
				app.name,
				app.namespace,
				err,
			)
		}
	}

	// Regardless of whether we just started an activation or found one already in
	// progress, we need to wait for that activation to be completed... or fail...
	// or time out.
	select {
	case <-appActivation.successCh:
		return app.targetHost, app.targetPort, nil
	case <-appActivation.timeoutCh:
		return "", 0, fmt.Errorf(
			"Timed out waiting for activation of %s %s in namespace %s: %s",
			app.kind,
			app.name,
			app.namespace,
			err,
		)
	}
}
