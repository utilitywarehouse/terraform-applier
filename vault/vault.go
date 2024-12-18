package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	vaultapi "github.com/hashicorp/vault/api"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"k8s.io/apimachinery/pkg/util/wait"
)

//go:generate go run github.com/golang/mock/mockgen -package vault -destination vault_mock.go github.com/utilitywarehouse/terraform-applier/vault ProviderInterface
type ProviderInterface interface {
	GenerateAWSCreds(ctx context.Context, jwt string, awsReq *tfaplv1beta1.VaultAWSRequest) (*AWSCredentials, error)
	GenerateGCPToken(ctx context.Context, jwt string, gcpReq *tfaplv1beta1.VaultGCPRequest) (string, error)
}

type Provider struct {
	AWSSecretsEngPath string
	GCPSecretsEngPath string
	AuthPath          string
}

// newClient returns pre configured vault api client
// since vault secrets is set on the client, runner should get new client on Each run
func newClient() (*vaultapi.Client, error) {
	vaultConfig := vaultapi.DefaultConfig()

	var envCACert string
	var envCAPath string

	if v := os.Getenv(vaultapi.EnvVaultCACert); v != "" {
		envCACert = v
	}

	if v := os.Getenv(vaultapi.EnvVaultCAPath); v != "" {
		envCAPath = v
	}

	// use custom cert if set
	if envCACert != "" || envCAPath != "" {
		err := vaultConfig.ConfigureTLS(&vaultapi.TLSConfig{
			CACert: envCACert,
			CAPath: envCAPath,
		})
		if err != nil {
			return nil, err
		}
	}

	vaultClient, err := vaultapi.NewClient(vaultConfig)
	if err != nil {
		return nil, err
	}

	return vaultClient, nil
}

func login(client *vaultapi.Client, kubeAuthPath, jwt, authRole string) error {
	loginPath := kubeAuthPath + "/login"
	secret, err := client.Logical().Write(loginPath, map[string]interface{}{
		"jwt":  jwt,
		"role": authRole,
	})
	if err != nil {
		return err
	}

	if secret == nil {
		return fmt.Errorf("no secret returned by %s", loginPath)
	}
	if secret.Auth == nil {
		return fmt.Errorf("no authentication information attached to the response from %s", loginPath)
	}
	client.SetToken(secret.Auth.ClientToken)

	return nil
}

var retriableError = func(err error) bool {
	var respErr *vaultapi.ResponseError
	if errors.As(err, &respErr) {
		//https://developer.hashicorp.com/vault/api-docs#http-status-codes
		if respErr.StatusCode == 400 ||
			respErr.StatusCode == 403 ||
			respErr.StatusCode == 404 {
			return false
		}
	}
	// re-try on all other error
	return true
}

// callWithBackOff uses wait.ExponentialBackoffWithContext to call given function exponentially
// on recoverable error
func callWithBackOff(ctx context.Context, fn func(ctx context.Context) error) error {

	var retry = wait.Backoff{
		Steps:    5,
		Duration: 5 * time.Second,
		Factor:   2,
		Jitter:   0.5,
	}

	var apiErr error
	err := wait.ExponentialBackoffWithContext(ctx, retry, func(ctx context.Context) (bool, error) {
		apiErr = fn(ctx)
		switch {
		case apiErr == nil:
			return true, nil
		case retriableError(apiErr):
			return false, nil
		default:
			return false, apiErr
		}
	})
	if wait.Interrupted(err) {
		err = apiErr
	}
	return err
}
