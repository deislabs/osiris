package healthz

import (
	"net/http"

	"github.com/golang/glog"
)

func HandleHealthCheckRequest(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte("{}")); err != nil {
		glog.Errorf("error writing health check response: %s", err)
	}
}
