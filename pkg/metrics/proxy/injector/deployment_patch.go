package injector

import (
	"encoding/json"

	"github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/golang/glog"
	"k8s.io/api/admission/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
)

func (i *injector) getDeploymentPatchOperations(
	ar *v1beta1.AdmissionReview,
) ([]kubernetes.PatchOperation, error) {
	req := ar.Request
	var deployment appsv1.Deployment
	if err := json.Unmarshal(req.Object.Raw, &deployment); err != nil {
		glog.Errorf("Could not unmarshal raw object: %v", err)
		return nil, err
	}

	glog.Infof(
		"AdmissionReview for Kind=%v, Namespace=%v Name=%v (%v) UID=%v "+
			"patchOperation=%v UserInfo=%v",
		req.Kind,
		req.Namespace,
		req.Name,
		deployment.Name,
		req.UID,
		req.Operation,
		req.UserInfo,
	)

	patchOps := []kubernetes.PatchOperation{}

	// Deployment is Osiris-enabled... make it so...
	if kubernetes.ResourceIsOsirisEnabled(deployment.Annotations) {

		// In this case, the deployment has no annotations. Add an empty map.
		if len(deployment.Spec.Template.Annotations) == 0 {
			patchOps = append(patchOps, kubernetes.PatchOperation{
				Op:    "add",
				Path:  "/spec/template/metadata/annotations",
				Value: map[string]string{},
			})
		}

		// Add or update "osiris.kubernetes.io/enabled"
		var op string
		if _, ok :=
			deployment.Spec.Template.Annotations["osiris.kubernetes.io/enabled"]; ok {
			op = "replace"
		} else {
			op = "add"
		}
		patchOps = append(patchOps, kubernetes.PatchOperation{
			Op:    op,
			Path:  "/spec/template/metadata/annotations/osiris.kubernetes.io~1enabled", // nolint: lll
			Value: "true",
		})

		return patchOps, nil

	}

	// If we get to here, Osiris is disabled... make it so...

	// There are no annotations... done.
	if len(deployment.Spec.Template.Annotations) == 0 {
		return nil, nil
	}

	// Annotations exists, and "osiris.kubernetes.io/enabled" exists-- remove it
	if _, ok :=
		deployment.Spec.Template.Annotations["osiris.kubernetes.io/enabled"]; ok {
		patchOps = append(patchOps, kubernetes.PatchOperation{
			Op:   "remove",
			Path: "/spec/template/metadata/annotations/osiris.kubernetes.io~1enabled",
		})
	}

	return patchOps, nil

}
