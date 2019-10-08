package activator

type appKind string

const (
	appKindDeployment  appKind = "Deployment"
	appKindStatefulSet appKind = "StatefulSet"
)

type app struct {
	namespace   string
	serviceName string
	name        string
	kind        appKind
	targetHost  string
	targetPort  int
}
