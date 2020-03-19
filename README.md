# Osiris - A general purpose, Scale to Zero component for Kubernetes

[![CircleCI](https://circleci.com/gh/deislabs/osiris/tree/master.svg?style=svg)](https://circleci.com/gh/deislabs/osiris/tree/master)

Osiris enables greater resource efficiency within a Kubernetes cluster by
allowing idling workloads to automatically scale-to-zero and allowing
scaled-to-zero workloads to be automatically re-activated on-demand by inbound
requests.

__Osiris, as a concept, is highly experimental and currently remains under heavy
development.__

## How it works

Various types of Kubernetes resources can be Osiris-enabled using an annotation.

Osiris-enabled pods are automatically instrumented with a __metrics-collecting
proxy__ deployed as a sidecar container.

Osiris-enabled deployments (if _already_ scaled to a configurable minimum number
of replicas-- one by default) automatically have metrics from their pods
continuously scraped and analyzed by the __zeroscaler__ component. When the
aggregated metrics reveal that all of the deployment's pods are idling, the
zeroscaler scales the deployment to zero replicas.

Under normal circumstances, scaling a deployment to zero replicas poses a
problem: any services that select pods from that deployment (and only that
deployment) would lose _all_ of their endpoints and become permanently
unavailable. Osiris-enabled services, however, have their endpoints managed by
the Osiris __endpoints controller__ (instead of Kubernetes' built-in endpoints
controller). The Osiris endpoints controller will automatically add Osiris
__activator__ endpoints to any Osiris-enabled service that has lost the rest of
its endpoints.

The Osiris __activator__ component receives traffic for Osiris-enabled services
that are lacking any application endpoints. The activator initiates a scale-up
of a corresponding deployment to a configurable minimum number of replicas (one,
by default). When at least one application pod becomes ready, the request will
be forwarded to the pod.

After the activator "reactivates" the deployment, the __endpoints controller__
(described above) will naturally observe the availability of application
endpoints for any Osiris-enabled services that select those pods and will
remove activator endpoints from that service. All subsequent traffic for the
service will, once again, flow directly to application pods... until a period of
inactivity causes the zeroscaler to take the application offline again.

### Scaling to zero and the HPA

Osiris is designed to work alongside the [Horizontal Pod Autoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/) and
is not meant to replace it-- it will scale your pods from n to 0 and from 0 to
n, where n is a configurable minimum number of replicas (one, by default). All
_other_ scaling decisions may be delegated to an HPA, if desired.

This diagram better illustrates the different roles of Osiris, the HPA and the
Cluster Autoscaler:

![diagram](diagram.svg)

## Setup

Prerequisites:

* [Helm](https://docs.helm.sh/using_helm/#installing-helm) (v3.0.0 or greater)
* A running Kubernetes cluster.

### Installation

Osiris' Helm chart is hosted in an
[Azure Container Registry](https://azure.microsoft.com/en-us/services/container-registry/),
which does not yet support anonymous access to charts therein. Until this is
resolved, adding the Helm repository from which Osiris can be installed requires
use of a shared set of read-only credentials.

Make sure helm is initialized in your running kubernetes cluster.

For more details on initializing helm, Go [here](https://docs.helm.sh/helm/#helm)

```
helm repo add osiris https://osiris.azurecr.io/helm/v1/repo \
  --username eae9749a-fccf-4a24-ac0d-6506fe2a6ab3 \
  --password =s-e.2-84BhIo6LM6=/l4C_sFzxb=sT[
```

Installation requires use of the `--devel` flag to indicate pre-release versions
of the specified chart are eligible for download and installation. The following
command install the latest version of Osiris with the default values for all
options - see the next section for all available installation options.

```
kubectl create namespace osiris-system
helm install osiris osiris/osiris-edge \
  --namespace osiris-system \
  --devel
```

#### Installation Options

Osiris global configuration is minimal - because most of it will be done by the users
with annotations on the Kubernetes resources.

The following table lists the configurable parameters of the Helm chart and their default values.

| Parameter | Description | Default |
| --------- | ----------- | ------- |
| `zeroscaler.metricsCheckInterval` | The interval in which the zeroScaler would repeatedly track the pod http request metrics. The value is the number of seconds of the interval. Note that this can also be set on a per-deployment basis, with an annotation. | `150` |

Example of installation with Helm and a custom configuration:

```
kubectl create namespace osiris-system
helm install osiris osiris/osiris-edge \
  --namespace osiris-system \
  --devel \
  --set zeroscaler.metricsCheckInterval=600
```

## Usage

Osiris will not affect the normal behavior of any Kubernetes resource without
explicitly being directed to do so.

To enabled the zeroscaler to scale a deployment with idling pods to zero
replicas, annotate the deployment like so:

```
apiVersion: apps/v1
kind: Deployment
metadata:
  namespace: my-aoo
  name: my-app
  annotations:
    osiris.deislabs.io/enabled: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: nginx
      annotations:
        osiris.deislabs.io/enabled: "true"
    # ...
  # ...
```

Note that the template for the pod _also_ uses an annotation to enable Osiris--
in this case, it enables the metrics-collecting proxy sidecar container on all
of the deployment's pods.

In Kubernetes, there is no direct relationship between deployments and services.
Deployments manage pods and services may select pods managed by one or more
deployments. Rather than attempt to infer relationships between deployments and
services and potentially impact service behavior without explicit consent,
Osiris requires services to explicitly opt-in to management by the Osiris
endpoints controller. Such services must also utilize an annotation to indicate
which deployment should be reactivated when the activator component intercepts a
request on their behalf. For example:

```
kind: Service
apiVersion: v1
metadata:
  namespace: my-namespace
  name: my-app
  annotations:
    osiris.deislabs.io/enabled: "true"
    osiris.deislabs.io/deployment: my-app
spec:
  selector:
    app: my-app
  # ...
```

### Configuration

Most of Osiris configuration is done with Kubernetes annotations - as seen in the Usage section.

#### Deployment Annotations

The following table lists the supported annotations for Kubernetes `Deployments` and their default values.

| Annotation | Description | Default |
| ---------- | ----------- | ------- |
| `osiris.deislabs.io/enabled` | Enable the zeroscaler component to scrape and analyze metrics from the deployment's pods and scale the deployment to zero when idle. Allowed values: `y`, `yes`, `true`, `on`, `1`. | _no value_ (= disabled) |
| `osiris.deislabs.io/minReplicas` | The minimum number of replicas to set on the deployment when Osiris will scale up. If you set `2`, Osiris will scale the deployment from `0` to `2` replicas directly. Osiris won't collect metrics from deployments which have more than `minReplicas` replicas - to avoid useless collections of metrics. | `1` |
| `osiris.deislabs.io/metricsCheckInterval` | The interval in which Osiris would repeatedly track the pod http request metrics. The value is the number of seconds of the interval. Note that this value override the global value defined by the `zeroscaler.metricsCheckInterval` Helm value. | _value of the `zeroscaler.metricsCheckInterval` Helm value_ |

#### Pod Annotations

The following table lists the supported annotations for Kubernetes `Pods` and their default values.

| Annotation | Description | Default |
| ---------- | ----------- | ------- |
| `osiris.deislabs.io/enabled` | Enable the metrics collecting proxy sidecar container to be injected into this pod. Allowed values: `y`, `yes`, `true`, `on`, `1`. | _no value_ (= disabled) |
| `osiris.deislabs.io/ignoredPaths` | The list of (url) paths that should be "ignored" by Osiris. Requests to such paths won't be "counted" by the proxy. Format: comma-separated string. | _no value_ |

#### Service Annotations

The following table lists the supported annotations for Kubernetes `Services` and their default values.

| Annotation | Description | Default |
| ---------- | ----------- | ------- |
| `osiris.deislabs.io/enabled` | Enable this service's endpoints to be managed by the Osiris endpoints controller. Allowed values: `y`, `yes`, `true`, `on`, `1`. | _no value_ (= disabled) |
| `osiris.deislabs.io/deployment` | Name of the deployment which is behind this service. This is _required_ to map the service with its deployment. | _no value_ |
| `osiris.deislabs.io/loadBalancerHostname` | Map requests coming from a specific hostname to this service. Note that if you have multiple hostnames, you can set them with different annotations, using `osiris.deislabs.io/loadBalancerHostname-1`, `osiris.deislabs.io/loadBalancerHostname-2`, ... | _no value_ |
| `osiris.deislabs.io/ingressHostname` | Map requests coming from a specific hostname to this service. If you use an ingress in front of your service, this is required to create a link between the ingress and the service. Note that if you have multiple hostnames, you can set them with different annotations, using `osiris.deislabs.io/ingressHostname-1`, `osiris.deislabs.io/ingressHostname-2`, ... | _no value_ |
| `osiris.deislabs.io/ingressDefaultPort` | Custom service port when the request comes from an ingress. Default behaviour if there are more than 1 port on the service, is to look for a port named `http`, and fallback to the port `80`. Set this if you have multiple ports and using a non-standard port with a non-standard name. | _no value_ |
| `osiris.deislabs.io/tlsPort` | Custom port for TLS-secured requests. Default behaviour if there are more than 1 port on the service, is to look for a port named `https`, and fallback to the port `443`. Set this if you have multiple ports and using a non-standard TLS port with a non-standard name. | _no value_ |

Note that you might see an `osiris.deislabs.io/selector` annotation - this is for internal use only, and you shouldn't try to set/update or delete it.

### Demo

Deploy the [example application](example/hello-osiris.yaml) `hello-osiris` :

```
kubectl create -f ./example/hello-osiris.yaml
```

This will create an Osiris-enabled deployment and service named `hello-osiris`.

Get the External IP of the `hello-osiris` service once it appears:

```
kubectl get service hello-osiris -o jsonpath='{.status.loadBalancer.ingress[*].ip}'
```

Point your browser to `"http://<EXTERNAL-IP>"`, and verify that
`hello-osiris` is serving traffic.

After about 2.5 minutes, the Osiris-enabled deployment should scale to zero
replicas and the one `hello-osiris` pod should be terminated.

Make a request again, and watch as Osiris scales the deployment back to one
replica and your request is handled successfully.

## Limitations

It is a specific goal of Osiris to enable greater resource efficiency within
Kubernetes clusters, in general, but especially with respect to "nodeless"
Kubernetes options such as
[Virtual Kubelet](https://github.com/virtual-kubelet/virtual-kubelet) or
[Azure Kubernetes Service Virtual Nodes](https://docs.microsoft.com/en-us/azure/aks/virtual-nodes-portal), however,
due to known issues with those technologies, Osiris remains incompatible with
them for the near term.

## Contributing

Osiris follows the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md).
