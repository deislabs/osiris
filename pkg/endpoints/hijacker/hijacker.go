package hijacker

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/deislabs/osiris/pkg/healthz"
	"github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/golang/glog"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

const port = 5000

// Hijacker is an interface for a component that handles webhook requests
// for patching Osiris-enabled services in a manner that will permit the
// Osiris endpoints controller to manage service endpoints
type Hijacker interface {
	// Run causes the webhook server to serve requests. This function will not
	// return until the context it has been passed expires or is canceled.
	Run(ctx context.Context)
}

// hijacker is a component that handles webhook requests for patching
// Osiris-enabled services in a manner that will permit the Osiris endpoints
// controller to manage service endpoints
type hijacker struct {
	config       Config
	deserializer runtime.Decoder
	srv          *http.Server
}

// NewHijacker returns a new component that handles webhook requests for
// patching Osiris-enabled services in a manner that will permit the Osiris
// endpoints controller to manage service endpoints
func NewHijacker(config Config) Hijacker {
	mux := http.NewServeMux()

	h := &hijacker{
		config: config,
		deserializer: serializer.NewCodecFactory(
			runtime.NewScheme(),
		).UniversalDeserializer(),
		srv: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		},
	}

	mux.HandleFunc("/mutate", h.handleRequest)
	mux.HandleFunc("/healthz", healthz.HandleHealthCheckRequest)

	return h
}

// Run causes the webhook server to serve requests. This function will not
// return until the context it has been passed expires or is canceled.
func (h *hijacker) Run(ctx context.Context) {
	doneCh := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done(): // Context was canceled or expired
			glog.Info("Endpoints hijacker is shutting down")
			// Allow up to five seconds for requests in progress to be completed
			shutdownCtx, cancel := context.WithTimeout(
				context.Background(),
				time.Second*5,
			)
			defer cancel()
			h.srv.Shutdown(shutdownCtx) // nolint: errcheck
		case <-doneCh: // The server shut down on its own, perhaps due to error
		}
	}()

	glog.Infof(
		"Endpoints hijacker is listening on %s, patching Osiris-enabled "+
			"services",
		h.srv.Addr,
	)
	err := h.srv.ListenAndServeTLS(h.config.TLSCertFile, h.config.TLSKeyFile)
	if err != http.ErrServerClosed {
		glog.Errorf("Endpoints hijacker error: %s", err)
	}
	close(doneCh)
}

// handleRequest serves requets to mutate an Osiris-enabed service
func (h *hijacker) handleRequest(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		glog.Error("empty body")
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		glog.Errorf("Content-Type=%s, expect application/json", contentType)
		http.Error(
			w,
			"invalid Content-Type, expect `application/json`",
			http.StatusUnsupportedMediaType,
		)
		return
	}

	var admissionResponse *v1beta1.AdmissionResponse
	var patchOps []kubernetes.PatchOperation
	var err error
	ar := v1beta1.AdmissionReview{}
	if _, _, err = h.deserializer.Decode(body, nil, &ar); err != nil {
		glog.Errorf("Can't decode body: %v", err)
	} else {
		svc := &corev1.Service{}
		if err = json.Unmarshal(ar.Request.Object.Raw, svc); err != nil {
			glog.Errorf("Could not unmarshal raw object: %v", err)
		} else {
			glog.Infof(
				"AdmissionReview for Kind=%v, Namespace=%v Name=%v (%v) UID=%v "+
					"patchOperation=%v UserInfo=%v",
				ar.Request.Kind,
				ar.Request.Namespace,
				ar.Request.Name,
				svc.Name,
				ar.Request.UID,
				ar.Request.Operation,
				ar.Request.UserInfo,
			)
			if err = validateService(svc); err != nil {
				glog.Errorf("Error validating service: %v", err)
			} else {
				patchOps, err = getServicePatchOperations(svc)
			}
		}
	}

	if err != nil {
		admissionResponse = &v1beta1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else if len(patchOps) == 0 {
		admissionResponse = &v1beta1.AdmissionResponse{
			Allowed: true,
		}
	} else {
		var patchBytes []byte
		patchBytes, err = json.Marshal(patchOps)
		if err != nil {
			admissionResponse = &v1beta1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		} else {
			glog.Infof("AdmissionResponse: patch=%v\n", string(patchBytes))
			admissionResponse = &v1beta1.AdmissionResponse{
				Allowed: true,
				Patch:   patchBytes,
				PatchType: func() *v1beta1.PatchType {
					pt := v1beta1.PatchTypeJSONPatch
					return &pt
				}(),
			}
		}
	}

	admissionReview := v1beta1.AdmissionReview{}
	if admissionResponse != nil {
		admissionReview.Response = admissionResponse
		if ar.Request != nil {
			admissionReview.Response.UID = ar.Request.UID
		}
	}

	resp, err := json.Marshal(admissionReview)
	if err != nil {
		glog.Errorf("Can't encode response: %v", err)
		http.Error(
			w,
			fmt.Sprintf("could not encode response: %v", err),
			http.StatusInternalServerError,
		)
	}
	glog.Infof("Ready to write response ...")
	if _, err := w.Write(resp); err != nil {
		glog.Errorf("Can't write response: %v", err)
		http.Error(
			w,
			fmt.Sprintf("could not write response: %v", err),
			http.StatusInternalServerError,
		)
	}
}

func validateService(svc *corev1.Service) error {
	if kubernetes.ResourceIsOsirisEnabled(svc.Annotations) {
		if _, ok := svc.Annotations["osiris.deislabs.io/deployment"]; !ok {
			return fmt.Errorf(
				`Osiris-enabled service %s in namespace %s is lacking the required `+
					`"osiris.deislabs.io/deployment" annotation`,
				svc.Name,
				svc.Namespace,
			)
		}
	}
	return nil
}
