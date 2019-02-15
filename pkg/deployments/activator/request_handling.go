package activator

import (
	"fmt"

	"github.com/golang/glog"
)

func (a *activator) activateAndWait(hostname string) (string, int, error) {
	glog.Infof("Request received for for host %s", hostname)

	a.indicesLock.RLock()
	app, ok := a.appsByHost[hostname]
	a.indicesLock.RUnlock()
	if !ok {
		return "", 0, fmt.Errorf("No deployment found for host %s", hostname)
	}

	glog.Infof(
		"Deployment %s in namespace %s may require activation",
		app.deploymentName,
		app.namespace,
	)

	// Are we already activating the deployment in question?
	var err error
	deploymentKey := getKey(app.namespace, app.deploymentName)
	deploymentActivation, ok := a.deploymentActivations[deploymentKey]
	if ok {
		glog.Infof(
			"Found activation in-progress for deployment %s in namespace %s",
			app.deploymentName,
			app.namespace,
		)
	} else {
		func() {
			a.deploymentActivationsLock.Lock()
			defer a.deploymentActivationsLock.Unlock()
			// Some other goroutine could have initiated activation of this deployment
			// while we were waiting for the lock. Now that we have the lock, do we
			// still need to do this?
			deploymentActivation, ok = a.deploymentActivations[deploymentKey]
			if ok {
				glog.Infof(
					"Found activation in-progress for deployment %s in namespace %s",
					app.deploymentName,
					app.namespace,
				)
				return
			}
			glog.Infof(
				"Found NO activation in-progress for deployment %s in namespace %s",
				app.deploymentName,
				app.namespace,
			)
			// Initiate activation (or discover that it may already have been started
			// by another activator process)
			if deploymentActivation, err = a.activateDeployment(app); err != nil {
				return
			}
			// Add it to the index of in-flight activation
			a.deploymentActivations[deploymentKey] = deploymentActivation
			// But remove it from that index when it's complete
			go func() {
				deleteActivation := func() {
					a.deploymentActivationsLock.Lock()
					defer a.deploymentActivationsLock.Unlock()
					delete(a.deploymentActivations, deploymentKey)
				}
				select {
				case <-deploymentActivation.successCh:
					deleteActivation()
				case <-deploymentActivation.timeoutCh:
					deleteActivation()
				}
			}()
		}()
		if err != nil {
			return "", 0, fmt.Errorf(
				"Error activating deployment %s in namespace %s: %s",
				app.deploymentName,
				app.namespace,
				err,
			)
		}
	}

	// Regardless of whether we just started an activation or found one already in
	// progress, we need to wait for that activation to be completed... or fail...
	// or time out.
	select {
	case <-deploymentActivation.successCh:
		return app.targetHost, app.targetPort, nil
	case <-deploymentActivation.timeoutCh:
		return "", 0, fmt.Errorf(
			"Timed out waiting for activation of deployment %s in namespace %s: %s",
			app.deploymentName,
			app.namespace,
			err,
		)
	}
}
