package runner

import (
	"context"
	"fmt"
	"net"
	"os"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"
)

//go:generate go run github.com/golang/mock/mockgen -package runner -destination delegation_mock.go github.com/utilitywarehouse/terraform-applier/runner DelegateInterface

// this interface is for mock testing
type DelegateInterface interface {
	DelegateToken(ctx context.Context, kubeClt kubernetes.Interface, namespace, serviceAccount string) (string, error)
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

func (d *Delegate) DelegateToken(ctx context.Context, kubeClt kubernetes.Interface, namespace, serviceAccount string) (string, error) {
	token, err := sysutil.GetSAToken(ctx, kubeClt, namespace, serviceAccount)
	if err != nil {
		return "", fmt.Errorf(`unable to get delegate token "%s/%s" err:%w`, namespace, serviceAccount, err)
	}
	return token, nil
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

func fetchEnvVars(ctx context.Context, client kubernetes.Interface, module *tfaplv1beta1.Module, envVars []tfaplv1beta1.EnvVar) (map[string]string, error) {
	kvPairs := make(map[string]string)
	for _, env := range envVars {

		// use value str if specified and skip other checks
		if env.Value != "" {
			kvPairs[env.Name] = env.Value
			continue
		}

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

	creds, err := r.Vault.GenerateAWSCreds(ctx, jwt, module.Spec.VaultRequests.AWS)
	if err != nil {
		return err
	}
	envs["AWS_ACCESS_KEY_ID"] = creds.AccessKeyID
	envs["AWS_SECRET_ACCESS_KEY"] = creds.SecretAccessKey
	envs["AWS_SESSION_TOKEN"] = creds.Token
	return nil
}

func (r *Runner) generateVaultGCPToken(ctx context.Context, module *tfaplv1beta1.Module, jwt string, envs map[string]string) error {

	token, err := r.Vault.GenerateGCPToken(ctx, jwt, module.Spec.VaultRequests.GCP)
	if err != nil {
		return err
	}
	envs["GOOGLE_OAUTH_ACCESS_TOKEN"] = token
	return nil
}
