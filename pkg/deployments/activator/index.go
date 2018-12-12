package activator

import (
	"fmt"
	"net/http/httputil"
	"net/url"
	"regexp"

	"github.com/golang/glog"
)

// nolint: lll
var (
	loadBalancerHostnameAnnotationRegex = regexp.MustCompile(`^osiris\.io/loadBalancerHostname(?:-\d+)?$`)
	ingressHostnameAnnotationRegex      = regexp.MustCompile(`^osiris\.io/ingressHostname(?:-\d+)?$`)
)

// updateIndex builds an index that maps all the possible ways a service can be
// addressed to application info that encapsulates details like which deployment
// to activate and where to relay requests to after successful activation. The
// new index replaces any old/existing index.
func (a *activator) updateIndex() {
	appsByHost := map[string]*app{}
	for _, svc := range a.services {
		if deploymentName, ok :=
			svc.Annotations["osiris.kubernetes.io/deployment"]; ok {
			svcDNSName :=
				fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace)
			// Determine the "default" ingress port. When a request arrives at the
			// activator via an ingress conroller, the request's host header won't
			// indicate a port. After activation is complete, the activator needs to
			// forward the request to the service (which is now backed by application
			// endpoints). It's important to know which service port to forward the
			// request to.
			var ingressDefaultPort string
			// Start by seeing if a default port was explicitly specified.
			if ingressDefaultPort, ok =
				svc.Annotations["osiris.kubernetes.io/ingressDefaultPort"]; !ok {
				// If not specified, try to infer it.
				// If there's only one port, that's it.
				if len(svc.Spec.Ports) == 1 {
					ingressDefaultPort = fmt.Sprintf("%d", svc.Spec.Ports[0].Port)
				} else {
					// Look for a port named "http". If found, that's it. While we're
					// looping also look to see if the servie exposes port 80. If no port
					// is named "http", we'll assume 80 (if exposed) is the default port.
					var foundPort80 bool
					for _, port := range svc.Spec.Ports {
						if port.Name == "http" {
							ingressDefaultPort = fmt.Sprintf("%d", port.Port)
							break
						}
						if port.Port == 80 {
							foundPort80 = true
						}
					}
					if ingressDefaultPort == "" && foundPort80 {
						ingressDefaultPort = "80"
					}
				}
			}
			// For every port...
			for _, port := range svc.Spec.Ports {
				targetURL, err :=
					url.Parse(fmt.Sprintf("http://%s:%d", svc.Spec.ClusterIP, port.Port))
				if err != nil {
					glog.Errorf(
						"Error parsing target URL for service %s in namespace %s: %s",
						svc.Name,
						svc.Namespace,
						err,
					)
					continue
				}
				app := &app{
					namespace:           svc.Namespace,
					serviceName:         svc.Name,
					deploymentName:      deploymentName,
					targetURL:           targetURL,
					proxyRequestHandler: httputil.NewSingleHostReverseProxy(targetURL),
				}
				// If the port is 80, also index by hostname/IP sans port number...
				if port.Port == 80 {
					// kube-dns name
					appsByHost[svcDNSName] = app
					// cluster IP
					appsByHost[svc.Spec.ClusterIP] = app
					// external IPs
					for _, loadBalancerIngress := range svc.Status.LoadBalancer.Ingress {
						if loadBalancerIngress.IP != "" {
							appsByHost[loadBalancerIngress.IP] = app
						}
					}
					// Honor all annotations of the form
					// ^osiris\.io/loadBalancerHostname(?:-\d+)?$
					for k, v := range svc.Annotations {
						if loadBalancerHostnameAnnotationRegex.MatchString(k) {
							appsByHost[v] = app
						}
					}
				}
				if fmt.Sprintf("%d", port.Port) == ingressDefaultPort {
					// Honor all annotations of the form
					// ^osiris\.io/ingressHostname(?:-\d+)?$
					for k, v := range svc.Annotations {
						if ingressHostnameAnnotationRegex.MatchString(k) {
							appsByHost[v] = app
						}
					}
				}
				// Now index by hostname/IP:port...
				// kube-dns name
				appsByHost[fmt.Sprintf("%s:%d", svcDNSName, port.Port)] = app
				// cluster IP
				appsByHost[fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, port.Port)] = app
				// external IPs
				for _, loadBalancerIngress := range svc.Status.LoadBalancer.Ingress {
					if loadBalancerIngress.IP != "" {
						appsByHost[fmt.Sprintf("%s:%d", loadBalancerIngress.IP, port.Port)] = app // nolint: lll
					}
				}
				// Node honame/IP:node-port
				if port.NodePort != 0 {
					for nodeAddress := range a.nodeAddresses {
						appsByHost[fmt.Sprintf("%s:%d", nodeAddress, port.NodePort)] = app
					}
				}
				// Honor all annotations of the form
				// ^osiris\.io/loadBalancerHostname(?:-\d+)?$
				for k, v := range svc.Annotations {
					if loadBalancerHostnameAnnotationRegex.MatchString(k) {
						appsByHost[fmt.Sprintf("%s:%d", v, port.Port)] = app
					}
				}
			}
		}
	}
	a.appsByHost = appsByHost
}
