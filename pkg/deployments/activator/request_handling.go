package activator

import (
	"net/http"

	"github.com/golang/glog"
)

func (a *activator) handleRequest(
	w http.ResponseWriter,
	r *http.Request,
) {
	defer r.Body.Close()

	glog.Infof(
		"Request received for for host %s with URI %s",
		r.Host,
		r.RequestURI,
	)

	a.indicesLock.RLock()
	app, ok := a.appsByHost[r.Host]
	a.indicesLock.RUnlock()
	if !ok {
		glog.Infof("No deployment found for host %s", r.Host)
		a.returnError(w, http.StatusNotFound)
		return
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
			glog.Errorf(
				"Error activating deployment %s in namespace %s: %s",
				app.deploymentName,
				app.namespace,
				err,
			)
			a.returnError(w, http.StatusServiceUnavailable)
			return
		}
	}

	// Regardless of whether we just started an activation or found one already in
	// progress, we need to wait for that activation to be completed... or fail...
	// or time out.
	select {
	case <-deploymentActivation.successCh:
		glog.Infof("Passing request on to: %s", app.targetURL)
		app.proxyRequestHandler.ServeHTTP(w, r)
	case <-deploymentActivation.timeoutCh:
		a.returnError(w, http.StatusServiceUnavailable)
	}
}

func (a *activator) returnError(w http.ResponseWriter, statusCode int) {
	w.WriteHeader(statusCode)
	if _, err := w.Write([]byte{}); err != nil {
		glog.Errorf("Error writing response body: %s", err)
	}
}
