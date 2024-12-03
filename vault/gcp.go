package vault

import (
	"errors"
	"fmt"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

// GenerateCreds retrieves credentials from vault for the given vaulRole and aws role
func (p *Provider) GenerateGCPToken(jwt string, gcpReq *tfaplv1beta1.VaultGCPRequest) (string, error) {

	if gcpReq == nil {
		return "", fmt.Errorf("one of 'roleset', 'staticAccount' or 'impersonatedAccount' must be set to generate GCP access_token")
	}

	var path, vaultAccount string
	switch {
	case gcpReq.Roleset != "":
		vaultAccount = gcpReq.Roleset
		path = p.AWSSecretsEngPath + "/roleset/" + vaultAccount + "/token"

	case gcpReq.StaticAccount != "":
		vaultAccount = gcpReq.StaticAccount
		path = p.AWSSecretsEngPath + "/static-account/" + vaultAccount + "/token"

	case gcpReq.ImpersonatedAccount != "":
		vaultAccount = gcpReq.ImpersonatedAccount
		path = p.AWSSecretsEngPath + "/impersonated-account/" + vaultAccount + "/token"

	default:
		return "", fmt.Errorf("one of 'roleset', 'staticAccount' or 'impersonatedAccount' must be set to generate GCP access_token")
	}

	// get vault client and login using provided service account's jwt
	client, err := newClient()
	if err != nil {
		return "", err
	}

	// when https://github.com/utilitywarehouse/vault-kube-cloud-credentials is used
	// to create vault secret then the name of the auth role is same as vault secret role/account name.
	err = login(client, p.AuthPath, jwt, vaultAccount)
	if err != nil {
		return "", err
	}

	secret, err := client.Logical().Read(path)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", errors.New("secret returned by vault client is nil")
	}

	return secret.Data["token"].(string), nil
}
