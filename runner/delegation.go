package runner

import (
	"context"
	"fmt"
	"net"
	"os"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"
)

//go:generate go run github.com/golang/mock/mockgen -package runner -destination delegation_mock.go github.com/utilitywarehouse/terraform-applier/runner DelegateInterface

// this interface is for mock testing
type DelegateInterface interface {
	SetupDelegation(ctx context.Context, kubeClt kubernetes.Interface, module *tfaplv1beta1.Module) (kubernetes.Interface, error)
}

type Delegate struct{}

func (d *Delegate) SetupDelegation(ctx context.Context, kubeClt kubernetes.Interface, module *tfaplv1beta1.Module) (kubernetes.Interface, error) {
	delegateToken, err := delegateToken(ctx, kubeClt, module)
	if err != nil {
		return nil, fmt.Errorf("failed fetching delegate token err:%s", err)
	}

	// creates the in-cluster config
	config, err := inClusterDelegatedConfig(delegateToken)
	if err != nil {
		return nil, fmt.Errorf("unable to create in-cluster config err:%s", err)
	}
	return kubernetes.NewForConfig(config)
}

func delegateToken(ctx context.Context, kubeClt kubernetes.Interface, module *tfaplv1beta1.Module) (string, error) {
	secret, err := kubeClt.CoreV1().Secrets(module.Namespace).Get(ctx, module.Spec.DelegateServiceAccountSecretRef, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf(`unable to get delegate token secret "%s/%s" err:%w`, module.Namespace, module.Spec.DelegateServiceAccountSecretRef, err)
	}
	if secret.Type != corev1.SecretTypeServiceAccountToken {
		return "", fmt.Errorf(`secret "%s/%s" is not of type %s`, secret.Namespace, secret.Name, corev1.SecretTypeServiceAccountToken)
	}
	delegateToken, ok := secret.Data["token"]
	if !ok {
		return "", fmt.Errorf(`secret "%s/%s" does not contain key 'token'`, secret.Namespace, secret.Name)
	}
	return string(delegateToken), nil
}

// InClusterConfig returns a config object which uses the service account's token
// modified version of https://pkg.go.dev/k8s.io/client-go@v0.26.1/rest#InClusterConfig
func inClusterDelegatedConfig(token string) (*rest.Config, error) {
	const (
		rootCAFile = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	)
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if len(host) == 0 || len(port) == 0 {
		return nil, fmt.Errorf("KUBERNETES_SERVICE_HOST or KUBERNETES_SERVICE_PORT env not set")
	}

	tlsClientConfig := rest.TLSClientConfig{}

	if _, err := cert.NewPool(rootCAFile); err != nil {
		return nil, fmt.Errorf("expected to load root CA config from %s, but got err: %v", rootCAFile, err)
	} else {
		tlsClientConfig.CAFile = rootCAFile
	}

	return &rest.Config{
		Host:            "https://" + net.JoinHostPort(host, port),
		TLSClientConfig: tlsClientConfig,
		BearerToken:     token,
	}, nil
}

func fetchEnvVars(ctx context.Context, client kubernetes.Interface, module *tfaplv1beta1.Module, envVars []corev1.EnvVar) (map[string]string, error) {
	kvPairs := make(map[string]string)
	for _, env := range envVars {
		// its ok to copy value from env.value if not set it will be overridden
		kvPairs[env.Name] = env.Value

		if env.ValueFrom == nil {
			continue
		}

		if env.ValueFrom.ConfigMapKeyRef != nil {

			cm, err := client.CoreV1().ConfigMaps(module.Namespace).Get(ctx, env.ValueFrom.ConfigMapKeyRef.Name, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("unable to get valueFrom configMap:%s err:%w", env.ValueFrom.ConfigMapKeyRef.Name, err)
			}
			kvPairs[env.Name] = cm.Data[env.ValueFrom.ConfigMapKeyRef.Key]

		} else if env.ValueFrom.SecretKeyRef != nil {

			secret, err := client.CoreV1().Secrets(module.Namespace).Get(ctx, env.ValueFrom.SecretKeyRef.Name, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("unable to get valueFrom configMap:%s err:%w", env.ValueFrom.SecretKeyRef.Name, err)
			}
			kvPairs[env.Name] = string(secret.Data[env.ValueFrom.SecretKeyRef.Key])
		}

	}

	return kvPairs, nil
}
