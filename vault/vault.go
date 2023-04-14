package vault

import (
	"fmt"
	"os"

	vaultapi "github.com/hashicorp/vault/api"
)

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

func login(client *vaultapi.Client, kubeAuthPath, jwt, vaultRole string) error {
	loginPath := kubeAuthPath + "/login"
	secret, err := client.Logical().Write(loginPath, map[string]interface{}{
		"jwt":  jwt,
		"role": vaultRole,
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
