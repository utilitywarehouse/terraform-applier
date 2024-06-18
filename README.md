# terraform-applier

Heavily adapted from
[kube-applier](https://github.com/utilitywarehouse/kube-applier),
terraform-applier enables continuous deployment of Terraform code by applying
modules from a Git repository.

## Usage

### Module CRD

Terraform-applier module's run behaviour is controlled through the Module CRD. Refer to the code
or CRD yaml definition for details, an example with the default values is shown
below:

```yaml
apiVersion: terraform-applier.uw.systems/v1beta1
kind: Module
metadata:
  name: hello
spec:
  repoURL: git@github.com:utilitywarehouse/terraform-applier.git
  repoRef: master
  path: dev/hello
  schedule: "00 */1 * * *"
  planOnly: false
  pollInterval: 60
  runTimeout: 900
  delegateServiceAccountSecretRef: terraform-applier-delegate-token
  rbac:
    - role: Admin
      subjects:
        - name: user@email.com
          kind: User
        - name: some_group_name
          kind: Group
  backend:
    - name: bucket
      value: dev-terraform-state
    - name: region
      value: eu-west-1
    - name: key
      valueFrom:
        configMapKeyRef:
          name: hello-module-config
          key: bucket_key
  env:
    - name: AWS_REGION
      value: eu-west-1
    - name: "AWS_SECRET_ACCESS_KEY"
      valueFrom:
        secretKeyRef:
          name: hello-module-secrets
          key: AWS_SECRET_KEY
    - name: TF_APPLIER_STRONGBOX_KEYRING
      valueFrom:
        secretKeyRef:
          name: hello-module-secrets
          key: strongbox_keyring
  var:
    - name: image_id
      value: ami-abc123
    - name: availability_zone_names
      valueFrom:
        configMapKeyRef:
          name: hello-module-config
          key: availability_zone_names
```

See the documentation on the Module CRD
[spec](api/v1beta1/module_types.go)
for more details.

### Delegate ServiceAccount

To minimize access required by controller on other namespaces, the concept of a
delegate ServiceAccount is introduced. When fetching secrets and configmaps for the Module, terraform-applier will use the credentials defined in the Secret referenced by
`delegateServiceAccountSecretRef`. This is a ServiceAccount in the same
namespace as the Module itself and should typically be given only `GET` access to only the secrets and configmaps referenced in module CRD.

### ENV and VAR

The `envs` referenced in module will be set before the terraform run. this should not be used for any well known Terraform environment variables that are already covered in options. [more info](https://pkg.go.dev/github.com/hashicorp/terraform-exec@v0.18.1/tfexec#Terraform)

All referenced `vars` will be json encoded as key-value pair and written to temp file `*.auto.tfvars.json` in module's root folder. Terraform will load these vars during `plan` and `apply`.

### Terraform backend configuration

use `backend` to configure backend of the module. The key/value pair referenced in the module's `backend` will be set when initialising Terraform via `-backend-config="KEY=VALUE"` flag.
Please note `backend` doesn't setup new backend it only configures existing backend, please see [Partial Configuration](https://developer.hashicorp.com/terraform/language/settings/backends/configuration#partial-configuration) for more info.

### Private Module Source

Terraform installs modules from Git repositories by running `git clone`, and so it will respect any local Git configuration set on your system, including credentials.
Terraform applier supports SSH credentials to fetch modules from private repository. Admin can enable this by setting `--set-git-ssh-command` flag and mounting SSH key on controller (please see `Controller config`).
once this flag is enabled controller configures `GIT_SSH_COMMAND` env with correct private key and known-hosts file path. this env will be used by `git` to fetch private repo using SSH.
Since only SSH auth method is supported module source URL should indicate SSH protocol as shown...

```
module "consul" {
  source = "git@github.com:hashicorp/example.git"
}
module "storage" {
  source = "git::ssh://username@example.com/storage.git"
}
```

Since key is set on controller it can be used by ALL modules managed by the controller. Terraform applier doesn't support private key per module yet.

### Strongbox decryption

Terraform applier supports strongbox decryption, its triggered if `TF_APPLIER_STRONGBOX_KEYRING` EVN is set on module.
content of this ENV should be valid strongbox keyring file data which should include strongbox key used to encrypt secrets in the module.
TF Applier will also configure Git and Strongbox Home before running `init` to decrypt any encrypted file from remote base as well.

### RBAC

Terraform applier does user authentication using OIDC flow (see Controller config).
during oidc flow it requests `openid, email, groups` scopes to get user's email and groups info as part of `id_token`.
`rbac` section of module crd can be use to set list of Admins who's allowed to do `force run`.

```
rbac:
- role: Admin
  subjects:
  - name: user@email.com
    kind: User
  - name: some_group_name
    kind: Group
```

At the moment only "Admin" role is supported, value of subjects can be either `email address` of users as kind `User` or the group name as kind `Group`.

**If `OIDC Issuer` is not set then web server will skip authentication and all `force run` requests will be allowed.**

### Graceful shutdown

To make sure all terraform module run does complete in finite time `runTimeout` is added to the module spec.
default value is `900s` and MAX value is `1800s`. Terraform run `(init,plan and apply if required)` should finish in this time otherwise it will be forced shutdown.

If controller received TERM signal during a module run, then it will try and finish current stage of the run (either `init`, `plan` or `apply`) without the force shutdown. during this case it will not process next stage. eg. if TERM signal received during `plan` stage then
it will not do `apply` even if drift is detected.

Controller will force shutdown on current stage run if it takes more time then `TERMINATION_GRACE_PERIOD` set on controller.

### Git Sync

Terraform-applier uses [git-mirror](https://github.com/utilitywarehouse/git-mirror) package to sync git repositories.
This package supports mirroring multiple repositories and all available references.
Because of this terraform-applier can also support different revisions on same repo. it can be set in module CRD by `repoRef` field.
Use following config to add repositories. supported urls formats are
'git@host.xz:org/repo.git','ssh://git@host.xz/org/repo.git' or 'https://host.xz/org/repo.git'

```yaml
git_mirror:
  defaults:
    interval: 1m # defaults to 30s
    git_gc: always # defaults to always
    auth:
      ssh_key_path: /etc/git-secret/ssh # defaults to --git-ssh-key-file flag
      ssh_known_hosts_path: /etc/git-secret/known_hosts # defaults to --git-ssh-known-hosts
  repositories:
    - remote: git@github.com:utilitywarehouse/terraform-applier.git
    - remote: git@github.com:utilitywarehouse/other-repo.git
```

### Git PR Planner

Terraform-applier can run terraform plan for open Pull Requests and post plan run outputs as PR comments.
To enable that, terraform-applier does the following:

1. Receives a webhook from Github notifying about a change in open Pull Requests e.g. new PR created, new commit pushed, new comment posted, etc.
2. Requests more information from Github about the PR: list of commits, comments, files updated, etc.
3. If plan run needs to be executed due to new commit or user request via comments (`@terraform-applier plan <module name>`, the request gets verified and forwarded to the Terraform Runner
4. The run output gets posted to the PR comments as soon as run is finished and stored in Redis

Apart from listening to webhooks terraform-applier also runs polling jobs at a set interval (every 10 minutes by default). These jobs help making sure no webhooks were missed and there are no outstanding requests.

PR Planner feature is enabled by default, but can be disabled either for a specific module by setting `planOnPR` to `false` in the module spec, or by setting `DISABLE_PR_PLANNER` env var to `false` to be disabled entirely across all modules.

PR Planner config:

- `--disable-pr-planner (DISABLE_PR_PLANNER)` - (default: `false`) Disable PR planner feature across all modules
- `--pr-planner-interval (PR_PLANNER_INTERVAL)` - (default: `300`) The inverval at which terraform-applier polls Github for any open PRs.
- `--pr-planner-webhook-port (PR_PLANNER_WEBHOOK_PORT)` - (default: `":8083"`) Port to listen to for incoming Github webhooks
- `--github-token (GITHUB_TOKEN)` - (default: `""`) Github API personal access token that allows requesting information about open Pull Requests and post comments to these PRs.  
  Example permissions: `Read` access to metadata and contents, `Read and Write` access to issues, and pull requests.
- `--github-webhook-secret (GITHUB_WEBHOOK_SECRET)` - (default: `""`) User-defined secret that will be used to sign and authorise the incoming webhooks.  
  Example Github webhook settings:
  - Payload URL: `https://teerraform-applier.foo.bar/github-events`
  - Content Type: `application/json`
  - Secret: `<GITHUB_WEBHOOK_SECRET>`
  - Enable SSL verification: `true`
  - Events: `Issue comments`, `Pull requests`
  - Active: `true`

### Controller config

- `--repos-root-path (REPOS_ROOT_PATH)` - (default: `/src`) Absolute path to the directory containing all repositories of the modules.
  This dir will be cleared on start.
- `--config (TF_APPLIER_CONFIG)` - (default: `/config/config.yaml`) Path to the tf applier config file containing repository config.
- `--min-interval-between-runs (MIN_INTERVAL_BETWEEN_RUNS)` - (default: `60`) The minimum interval in seconds, user can set between 2 consecutive runs. This value defines the frequency of runs.
- `--termination-grace-period (TERMINATION_GRACE_PERIOD)` - (default: `60`) Termination grace period is the ime given to
  the running job to finish current run after 1st TERM signal is received. After this timeout runner will be forced to shutdown.
  Ideally this timeout should be just below the `terminationGracePeriodSeconds` set on controller pod.
- `--terraform-path (TERRAFORM_PATH)` - (default: `""`) The local path to a terraform
  binary to use.
- `--terraform-version (TERRAFORM_VERSION)` - (default: `""`) The version of terraform to
  use. The applier will install the requested release when it starts up. If you
  don't specify an explicit version, it will choose the latest available
  one. Ignored if `TERRAFORM_PATH` is set.
- `--set-git-ssh-command-global-env (SET_GIT_SSH_COMMAND_GLOBAL_ENV)` - (default: `false`) If set GIT_SSH_COMMAND env will be set as global env for all modules. This ssh command will be used by modules during terraform init to pull private remote modules.
- `--git-ssh-key-file (GIT_SSH_KEY_FILE)` - (default: `/etc/git-secret/ssh`) The path to git ssh key which will be used to setup GIT_SSH_COMMAND env.
- `--git-ssh-known-hosts-file (GIT_SSH_KNOWN_HOSTS_FILE)` - (default: `/etc/git-secret/known_hosts`) The local path to the known hosts file used to setup GIT_SSH_COMMAND env.
- `--git-verify-known-hosts (GIT_VERIFY_KNOWN_HOSTS)` - (default: `true`) The local path to the known hosts file used to setup GIT_SSH_COMMAND env.
- `--controller-runtime-env (CONTROLLER_RUNTIME_ENV)` - (default: `""`) The comma separated list of ENVs which will be passed from controller to all terraform run process. The envs should be set on the controller.
- `--cleanup-temp-dir` - (default: `false`) If set, the contents of the OS temporary directory and `/src` will be removed. This can help removing redundant terraform binaries and avoiding the directories growing in size with every restart.

---

- `--module-label-selector (MODULE_LABEL_SELECTOR)` - (default: `""`) If present controller will only watch and process modules with this label.
  Env value string should be in the form of 'label-key=label-value'. if multiple terraform-applier is running in same cluster
  and if any 1 of them is in cluster scope mode then this env `must` be set otherwise it will watch ALL modules and interfere
  with other controllers run.
- `--watch-namespaces (WATCH_NAMESPACES)` - (default: `""`) if set controller will only watch given namespaces for modules. it will operate
  in namespace scope mode and controller will not need any cluster permissions. if `label selector` also set then it will
  only watch modules with selector label in a given namespace.
- `--leader-elect (LEADER_ELECT)` - (default: `false`) Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.
- `--election-id (ELECTION_ID)` - (default: `auto generated`) it determines the name of the resource that leader election will use for holding the leader lock. if multiple controllers are running with same label selector and watch namespace value then they belong to same stack. if election enabled, ELECTION_ID needs to be unique per stack. If this is not unique to the stack then only one stack will be working concurrently. if not set value will be auto generated based on given label selector and watch namespace value.

---

- `--log-level (LOG_LEVEL)` - (default: `INFO`) `TRACE|DEBUG|INFO|WARN|ERROR`, case insensitive.
- `--webserver-bind-address` - (default: `8080`) The address the web server binds to.
- `--metrics-bind-address` - (default: `8081`) The address the metric endpoint binds to.
- `--health-probe-bind-address` - (default: `8082`) The address the probe endpoint binds to.

---

- `(VAULT_ADDR)` - (default: `""`) The Address of the Vault server expressed as a URL and port
- `(VAULT_CACERT)` - (default: `""`) The path to a PEM-encoded CA certificate file.
- `(VAULT_CAPATH)` - (default: `""`) The Path to a directory of PEM-encoded CA certificate files on the local disk.
- `--vault-aws-secret-engine-path (VAULT_AWS_SEC_ENG_PATH)` - (default: `/aws`) The path where AWS secrets engine is enabled.
- `--vault-kube-auth-path (VAULT_KUBE_AUTH_PATH)` - (default: `/auth/kubernetes`) The path where kubernetes auth method is mounted.

---

- `--oidc-callback-url (OIDC_CALLBACK_URL)` - (default: `""`) The callback url used for OIDC auth flow, this should be the terraform-applier url.
- `--oidc-client-id (OIDC_CLIENT_ID)` - (default: `""`) The client ID of the OIDC app.
- `--oidc-client-secret (OIDC_CLIENT_SECRET)` - (default: `""`) The client secret of the OIDC app.
- `--oidc-issuer (OIDC_ISSUER)` - (default: `""`) The url of the IDP where OIDC app is created.

**If `OIDC Issuer` is not set then web server will skip authentication and all `force run` requests will be allowed.**

## Kube backend

For modules using kubernetes backend or provider, ideally module should be using its own SA's token (terraform-applier-delegate-token) for authentication with kube cluster and not depend on default in cluster config of controller's SA but kube provider ignores `host` and `token` backend attributes if kube config is not set. [related issue](https://github.com/hashicorp/terraform/issues/31275)

controller creates a kube config at temp location and sets `KUBE_CONFIG_PATH` ENV for the module. this generated config contains server URL as well as cluster CA cert.
since `KUBE_CONFIG_PATH` is already set module just need to set `namespace` and `token`. token can be passed as ENV `KUBE_TOKEN`. [doc](https://developer.hashicorp.com/terraform/language/settings/backends/kubernetes)

```yaml
apiVersion: terraform-applier.uw.systems/v1beta1
kind: Module
metadata:
  name: hello-kube
spec:
  backend:
    - name: namespace
      value: sys-hello-kube
  env:
    - name: KUBE_TOKEN
      valueFrom:
        secretKeyRef:
          name: terraform-applier-delegate-token
          key: token
```

## Vault integration

terraform-applier supports fetching (generating) secrets from the vault. Module's delegated service account's jwt (secret:terraform-applier-delegate-token) will be used for vault login for given `vaultRole`. at the moment only aws secrets engine is supported.

```yaml
spec:
  vaultRequests:
    aws:
      // VaultRole Specifies the name of the vault role to generate credentials against.
      vaultRole: dev_aws_some-vault-role
      // Must be one of iam_user, assumed_role, or federation_token.
      credentialType: assumed_role
      // The ARN of the role to assume if credential_type on the Vault role is assumed_role.
      // Optional if the Vault role only allows a single AWS role ARN.
      roleARN: arn:aws:iam::00000000:role/sys-tf-applier-example
```

## Monitoring

### Metrics

terraform-applier exports Prometheus metrics. The metrics are available on given metrics port at `/metrics`.

In addition to the [controller-runtime](https://book.kubebuilder.io/reference/metrics-reference.html) default metrics, the following custom metrics are included:

- `terraform_applier_module_run_count` - (tags: `module`,`namespace`, `success`) A Counter for each module that has had a terraform run attempt over the lifetime of
  the application, incremented with each apply attempt and tagged with the result of the run (`success=true|false`)
- `terraform_applier_module_run_duration_seconds` - (tags: `module`,`namespace`, `success`) A Summary that keeps track of the durations of each terraform run for
  each module, tagged with the result of the run (`success=true|false`)
- `terraform_applier_module_last_run_success` - (tags: `module`,`namespace`) A `Gauge` which
  tracks whether the last terraform run for a module was successful.
- `terraform_applier_module_last_run_timestamp` - (tags: `module`,`namespace`) A Gauge that captures the Timestamp of the last successful module run.
- `terraform_applier_module_terraform_exit_code_count` - (tags: `module`,`namespace`, `command`, `exit_code`) A `Counter` for each exit code returned by executions of
  `terraform`, labelled with the command issued (`init`, `plan`,`apply`) and the exit code. It's worth noting that `plan` will
  return a code of `2` if there are changes to be made, which is not an error or a failure, so you may wish to account for this in your alerting.
- `terraform_applier_git_last_mirror_timestamp` - (tags: `repo`) A Gauge that captures the Timestamp of the last successful git sync per repo.
- `terraform_applier_git_mirror_count` - (tags: `repo`,`success`) A Counter for each repo sync, incremented with each sync attempt and tagged with the result (`success=true|false`)
- `terraform_applier_git_mirror_latency_seconds` - (tags: `repo`) A Summary that keeps track of the git sync latency per repo.
