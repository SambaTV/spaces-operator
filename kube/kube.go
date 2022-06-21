package kube

import (
	"os"

	"github.com/mitchellh/go-homedir"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// GetClient returns a new configured Kubernetes client.
func GetClient() (kubernetes.Interface, error) {
	var cfg *rest.Config
	var err error

	// Set the kubeconfig file to use.
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = "~/.kube/config"
	}
	if kubeconfig, err = homedir.Expand(kubeconfig); err != nil {
		return nil, err
	}

	// Set the kubernetes client config from the kubeconfig file.
	if !fileExists(kubeconfig) {
		cfg, err = rest.InClusterConfig()
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if err != nil {
		return nil, err
	}

	// Create new kubernetes client from its config.
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// fileExists tests if a file exists at path.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
