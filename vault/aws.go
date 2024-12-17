package vault

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/vault/api"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

// AWSCredentials are the credentials served by the API
type AWSCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	Token           string
	ARN             string
}

// GenerateCreds retrieves credentials from vault for the given vaulRole and aws role
func (p *Provider) GenerateAWSCreds(ctx context.Context, jwt string, awsReq *tfaplv1beta1.VaultAWSRequest) (*AWSCredentials, error) {

	if awsReq == nil || awsReq.VaultRole == "" {
		return nil, fmt.Errorf("vault role is required to generate aws credentials")
	}

	// Get a credentials secret from vault for the role
	var secretData map[string][]string
	if awsReq.RoleARN != "" {
		secretData = map[string][]string{
			"role_arn": {awsReq.RoleARN},
		}
	}

	path := p.AWSSecretsEngPath + "/sts/" + awsReq.VaultRole
	if awsReq.CredentialType == "iam_user" {
		path = p.AWSSecretsEngPath + "/creds/" + awsReq.VaultRole
	}

	var secret *api.Secret
	tryRead := func(ctx context.Context) error {
		// get vault client and login using provided service account's jwt
		// create new client to hot reload CA Cert
		client, err := newClient()
		if err != nil {
			return err
		}

		// when https://github.com/utilitywarehouse/vault-kube-cloud-credentials is used
		// to create vault secret then the name of the auth role is same as vault secret role/account.
		err = login(client, p.AuthPath, jwt, awsReq.VaultRole)
		if err != nil {
			return err
		}

		secret, err = client.Logical().ReadWithData(path, secretData)
		if err != nil {
			return err
		}
		return nil
	}

	if err := callWithBackOff(ctx, tryRead); err != nil {
		return nil, err
	}

	if secret == nil {
		return nil, errors.New("secret returned by vault client is nil")
	}

	return &AWSCredentials{
		ARN:             secret.Data["arn"].(string),
		AccessKeyID:     secret.Data["access_key"].(string),
		SecretAccessKey: secret.Data["secret_key"].(string),
		Token:           secret.Data["security_token"].(string),
	}, nil
}
