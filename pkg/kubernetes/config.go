package kubernetes

import (
	"github.com/kelseyhightower/envconfig"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const envconfigPrefix = "KUBE"

// config represents common configuration options for a Kubernetes client
type config struct {
	MasterURL      string `envconfig:"MASTER"`
	KubeConfigPath string `envconfig:"CONFIG"`
}

// Client returns a new Kubernetes client
func Client() (*kubernetes.Clientset, error) {
	c := config{}
	err := envconfig.Process(envconfigPrefix, &c)
	if err != nil {
		return nil, err
	}
	var cfg *rest.Config
	if c.MasterURL == "" && c.KubeConfigPath == "" {
		cfg, err = rest.InClusterConfig()
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags(c.MasterURL, c.KubeConfigPath)
	}
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}
