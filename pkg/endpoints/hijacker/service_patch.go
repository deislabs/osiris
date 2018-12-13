package hijacker

import (
	"encoding/base64"
	"encoding/json"

	"github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
)

// getServicePatchOperations returns a slice of patch operations that will
// ensures that an Osiris-enabled service is selector-less and that the
// would-be selector is encoded and stored in an annotation. The practical
// effect of this is that Kubernetes' built-in endpoints controller will not
// manage the endpoints object corresponding to this service-- thereby
// permitting the Osiris endpoints controller to provide that function instead.
// The Osiris endpoints controller will use the encoded, saved would-be selector
// to establish a watch on the very pods that the service would have selected
// itself were it not selector-less.
func getServicePatchOperations(
	svc *corev1.Service,
) ([]kubernetes.PatchOperation, error) {
	const osirisEnabledAnnotationPath = "/metadata/annotations/osiris.deislabs.io~1selector" // nolint: lll

	patchOps := []kubernetes.PatchOperation{}

	// Service is Osiris-enabled... make it so...
	if kubernetes.ResourceIsOsirisEnabled(svc.Annotations) {

		glog.Infof("Hijacking service %s", svc.Name)

		if len(svc.Spec.Selector) == 0 {
			return nil, nil
		}

		// Blow away the selector
		patchOps = append(patchOps, kubernetes.PatchOperation{
			Op:   "remove",
			Path: "/spec/selector",
		})

		// And "save" the selector using a new annotation
		selectorJSONBytes, err := json.Marshal(svc.Spec.Selector)
		if err != nil {
			return nil, err
		}
		encodedSelector := base64.StdEncoding.EncodeToString(selectorJSONBytes)
		var op string
		if _, ok := svc.Annotations["osiris.deislabs.io/selector"]; ok {
			op = "replace"
		} else {
			op = "add"
		}
		patchOps = append(patchOps, kubernetes.PatchOperation{
			Op:    op,
			Path:  osirisEnabledAnnotationPath,
			Value: encodedSelector,
		})

		return patchOps, nil
	}

	// Service is NOT Osiris-enabled... make it so...
	if _, ok := svc.Annotations["osiris.deislabs.io/selector"]; ok {
		patchOps = append(patchOps, kubernetes.PatchOperation{
			Op:   "remove",
			Path: osirisEnabledAnnotationPath,
		})
	}

	return patchOps, nil

}
