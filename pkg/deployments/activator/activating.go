package activator

import (
	"encoding/json"

	"github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8s_types "k8s.io/apimachinery/pkg/types"
)

func (a *activator) activateDeployment(
	app *app,
) (*deploymentActivation, error) {
	deploymentsClient := a.kubeClient.AppsV1().Deployments(app.namespace)
	deployment, err := deploymentsClient.Get(
		app.deploymentName,
		metav1.GetOptions{},
	)
	if err != nil {
		return nil, err
	}
	da := &deploymentActivation{
		readyAppPodIPs: map[string]struct{}{},
		successCh:      make(chan struct{}),
		timeoutCh:      make(chan struct{}),
	}
	glog.Infof(
		"Activating deployment %s in namespace %s",
		app.deploymentName,
		app.namespace,
	)
	go da.watchForCompletion(
		a.kubeClient,
		app,
		labels.Set(deployment.Spec.Selector.MatchLabels).AsSelector(),
	)
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas > 0 {
		// We don't need to do this, as it turns out! Scaling is either already
		// in progress-- perhaps initiated by another process-- or may even be
		// completed already. Just return dr and allow the caller to move on to
		// verifying / waiting for this activation to be complete.
		return da, nil
	}
	patches := []kubernetes.PatchOperation{{
		Op:    "replace",
		Path:  "/spec/replicas",
		Value: kubernetes.GetMinReplicas(deployment.Annotations, 1),
	}}
	patchesBytes, _ := json.Marshal(patches)
	_, err = deploymentsClient.Patch(
		app.deploymentName,
		k8s_types.JSONPatchType,
		patchesBytes,
	)
	return da, err
}
