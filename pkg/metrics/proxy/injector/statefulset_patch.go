package injector

import (
	"encoding/json"

	"github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/golang/glog"
	"k8s.io/api/admission/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
)

func (i *injector) getStatefulSetPatchOperations(
	ar *v1beta1.AdmissionReview,
) ([]kubernetes.PatchOperation, error) {
	req := ar.Request
	var statefulSet appsv1.StatefulSet
	if err := json.Unmarshal(req.Object.Raw, &statefulSet); err != nil {
		glog.Errorf("Could not unmarshal raw object: %v", err)
		return nil, err
	}

	glog.Infof(
		"AdmissionReview for Kind=%v, Namespace=%v Name=%v (%v) UID=%v "+
			"patchOperation=%v UserInfo=%v",
		req.Kind,
		req.Namespace,
		req.Name,
		statefulSet.Name,
		req.UID,
		req.Operation,
		req.UserInfo,
	)

	patchOps := []kubernetes.PatchOperation{}

	// StatefulSet is Osiris-enabled... make it so...
	if kubernetes.ResourceIsOsirisEnabled(statefulSet.Annotations) {

		// In this case, the statefulSet has no annotations. Add an empty map.
		if len(statefulSet.Spec.Template.Annotations) == 0 {
			patchOps = append(patchOps, kubernetes.PatchOperation{
				Op:    "add",
				Path:  "/spec/template/metadata/annotations",
				Value: map[string]string{},
			})
		}

		// Add or update "osiris.deislabs.io/enabled"
		var op string
		if _, ok :=
			statefulSet.Spec.Template.Annotations["osiris.deislabs.io/enabled"]; ok {
			op = "replace"
		} else {
			op = "add"
		}
		patchOps = append(patchOps, kubernetes.PatchOperation{
			Op:    op,
			Path:  "/spec/template/metadata/annotations/osiris.deislabs.io~1enabled", // nolint: lll
			Value: "true",
		})

		return patchOps, nil

	}

	// If we get to here, Osiris is disabled... make it so...

	// There are no annotations... done.
	if len(statefulSet.Spec.Template.Annotations) == 0 {
		return nil, nil
	}

	// Annotations exists, and "osiris.deislabs.io/enabled" exists-- remove it
	if _, ok :=
		statefulSet.Spec.Template.Annotations["osiris.deislabs.io/enabled"]; ok {
		patchOps = append(patchOps, kubernetes.PatchOperation{
			Op:   "remove",
			Path: "/spec/template/metadata/annotations/osiris.deislabs.io~1enabled",
		})
	}

	return patchOps, nil

}
