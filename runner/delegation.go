package runner

import (
	"context"
	"fmt"
	"net"
	"os"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"
)

//go:generate go run github.com/golang/mock/mockgen -package runner -destination delegation_mock.go github.com/utilitywarehouse/terraform-applier/runner DelegateInterface

// this interface is for mock testing
type DelegateInterface interface {
	DelegateToken(ctx context.Context, kubeClt kubernetes.Interface, module *tfaplv1beta1.Module) (string, error)
	SetupDelegation(ctx context.Context, jwt string) (kubernetes.Interface, error)
}

type Delegate struct{}

func (d *Delegate) SetupDelegation(ctx context.Context, jwt string) (kubernetes.Interface, error) {
	// creates the in-cluster config
	config, err := inClusterDelegatedConfig(jwt)
	if err != nil {
		return nil, fmt.Errorf("unable to create in-cluster config err:%s", err)
	}
	return kubernetes.NewForConfig(config)
}

func (d *Delegate) DelegateToken(ctx context.Context, kubeClt kubernetes.Interface, module *tfaplv1beta1.Module) (string, error) {
	secret, err := sysutil.GetSecret(ctx, kubeClt, module.Namespace, module.Spec.DelegateServiceAccountSecretRef)
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
			cm, err := sysutil.GetConfigMaps(ctx, client, module.Namespace, env.ValueFrom.ConfigMapKeyRef.Name)
			if err != nil {
				return nil, fmt.Errorf("unable to get valueFrom configMap:%s err:%w", env.ValueFrom.ConfigMapKeyRef.Name, err)
			}
			kvPairs[env.Name] = cm.Data[env.ValueFrom.ConfigMapKeyRef.Key]

		} else if env.ValueFrom.SecretKeyRef != nil {
			secret, err := sysutil.GetSecret(ctx, client, module.Namespace, env.ValueFrom.SecretKeyRef.Name)
			if err != nil {
				return nil, fmt.Errorf("unable to get valueFrom configMap:%s err:%w", env.ValueFrom.SecretKeyRef.Name, err)
			}
			kvPairs[env.Name] = string(secret.Data[env.ValueFrom.SecretKeyRef.Key])
		}

	}

	return kvPairs, nil
}

func (r *Runner) generateVaultAWSCreds(ctx context.Context, module *tfaplv1beta1.Module, jwt string, envs map[string]string) error {

	creds, err := r.AWSSecretsEngineConfig.GenerateCreds(jwt, module.Spec.VaultRequests.AWS)
	if err != nil {
		return err
	}
	envs["AWS_ACCESS_KEY_ID"] = creds.AccessKeyID
	envs["AWS_SECRET_ACCESS_KEY"] = creds.SecretAccessKey
	envs["AWS_SESSION_TOKEN"] = creds.Token
	return nil
}
