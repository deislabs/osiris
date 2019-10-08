package activator

import (
	"fmt"
	"regexp"
)

// nolint: lll
var (
	loadBalancerHostnameAnnotationRegex = regexp.MustCompile(`^osiris\.deislabs\.io/loadBalancerHostname(?:-\d+)?$`)
	ingressHostnameAnnotationRegex      = regexp.MustCompile(`^osiris\.deislabs\.io/ingressHostname(?:-\d+)?$`)
)

// updateIndex builds an index that maps all the possible ways a service can be
// addressed to application info that encapsulates details like which deployment
// or statefulSet to activate and where to relay requests to after successful
// activation. The new index replaces any old/existing index.
func (a *activator) updateIndex() {
	appsByHost := map[string]*app{}
	for _, svc := range a.services {
		var (
			name string
			kind appKind
		)
		if deploymentName, ok :=
			svc.Annotations["osiris.deislabs.io/deployment"]; ok {
			name = deploymentName
			kind = appKindDeployment
		} else if statefulSetName, ok :=
			svc.Annotations["osiris.deislabs.io/statefulset"]; ok {
			name = statefulSetName
			kind = appKindStatefulSet
		}
		if len(name) == 0 {
			continue
		}

		svcShortDNSName := fmt.Sprintf("%s.%s", svc.Name, svc.Namespace)
		svcFullDNSName := fmt.Sprintf("%s.svc.cluster.local", svcShortDNSName)
		// Determine the "default" ingress port. When a request arrives at the
		// activator via an ingress conroller, the request's host header won't
		// indicate a port. After activation is complete, the activator needs to
		// forward the request to the service (which is now backed by application
		// endpoints). It's important to know which service port to forward the
		// request to.
		var ingressDefaultPort string
		var ok bool
		// Start by seeing if a default port was explicitly specified.
		if ingressDefaultPort, ok =
			svc.Annotations["osiris.deislabs.io/ingressDefaultPort"]; !ok {
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
		// Determine the "default" TLS port. When a TLS-secured request arrives at
		// the activator, the TLS SNI header won't indicate a port. After
		// activation is complete, the activator needs to forward the request to
		// the service (which is now backed by application endpoints). It's
		// important to know which service port to forward the request to.
		var tlsDefaultPort string
		if tlsDefaultPort, ok =
			svc.Annotations["osiris.deislabs.io/tlsPort"]; !ok {
			// If not specified, try to infer it.
			// If there's only one port, that's it.
			if len(svc.Spec.Ports) == 1 {
				tlsDefaultPort = fmt.Sprintf("%d", svc.Spec.Ports[0].Port)
			} else {
				// Look for a port named "https". If found, that's it. While we're
				// looping also look to see if the servie exposes port 443. If no port
				// is named "https", we'll assume 443 (if exposed) is the default
				// port.
				var foundPort443 bool
				for _, port := range svc.Spec.Ports {
					if port.Name == "https" {
						tlsDefaultPort = fmt.Sprintf("%d", port.Port)
						break
					}
					if port.Port == 443 {
						foundPort443 = true
					}
				}
				if tlsDefaultPort == "" && foundPort443 {
					ingressDefaultPort = "443"
				}
			}
		}
		// For every port...
		for _, port := range svc.Spec.Ports {
			app := &app{
				namespace:   svc.Namespace,
				serviceName: svc.Name,
				name:        name,
				kind:        kind,
				targetHost:  svc.Spec.ClusterIP,
				targetPort:  int(port.Port),
			}
			// If the port is 80, also index by hostname/IP sans port number...
			if port.Port == 80 {
				// kube-dns names
				appsByHost[svcShortDNSName] = app
				appsByHost[svcFullDNSName] = app
				// cluster IP
				appsByHost[svc.Spec.ClusterIP] = app
				// external IPs
				for _, loadBalancerIngress := range svc.Status.LoadBalancer.Ingress {
					if loadBalancerIngress.IP != "" {
						appsByHost[loadBalancerIngress.IP] = app
					}
				}
				// Honor all annotations of the form
				// ^osiris\.deislabs\.io/loadBalancerHostname(?:-\d+)?$
				for k, v := range svc.Annotations {
					if loadBalancerHostnameAnnotationRegex.MatchString(k) {
						appsByHost[v] = app
					}
				}
			}
			if fmt.Sprintf("%d", port.Port) == ingressDefaultPort {
				// Honor all annotations of the form
				// ^osiris\.deislabs\.io/ingressHostname(?:-\d+)?$
				for k, v := range svc.Annotations {
					if ingressHostnameAnnotationRegex.MatchString(k) {
						appsByHost[v] = app
					}
				}
			}
			if fmt.Sprintf("%d", port.Port) == tlsDefaultPort {
				// Now index by hostname:tls. Note that there's no point in indexing
				// by IP:tls because SNI server name will never be an IP.
				// kube-dns names
				appsByHost[fmt.Sprintf("%s:tls", svcShortDNSName)] = app
				appsByHost[fmt.Sprintf("%s:tls", svcFullDNSName)] = app
				// Honor all annotations of the form
				// ^osiris\.deislabs\.io/loadBalancerHostname(?:-\d+)?$
				for k, v := range svc.Annotations {
					if loadBalancerHostnameAnnotationRegex.MatchString(k) {
						appsByHost[fmt.Sprintf("%s:tls", v)] = app
					}
				}
			}
			// Now index by hostname/IP:port...
			// kube-dns names
			appsByHost[fmt.Sprintf("%s:%d", svcShortDNSName, port.Port)] = app
			appsByHost[fmt.Sprintf("%s:%d", svcFullDNSName, port.Port)] = app
			// cluster IP
			appsByHost[fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, port.Port)] = app
			// external IPs
			for _, loadBalancerIngress := range svc.Status.LoadBalancer.Ingress {
				if loadBalancerIngress.IP != "" {
					appsByHost[fmt.Sprintf("%s:%d", loadBalancerIngress.IP, port.Port)] = app // nolint: lll
				}
			}
			// Node hostname/IP:node-port
			if port.NodePort != 0 {
				for nodeAddress := range a.nodeAddresses {
					appsByHost[fmt.Sprintf("%s:%d", nodeAddress, port.NodePort)] = app
				}
			}
			// Honor all annotations of the form
			// ^osiris\.deislabs\.io/loadBalancerHostname(?:-\d+)?$
			for k, v := range svc.Annotations {
				if loadBalancerHostnameAnnotationRegex.MatchString(k) {
					appsByHost[fmt.Sprintf("%s:%d", v, port.Port)] = app
				}
			}
		}
	}
	a.appsByHost = appsByHost
}
