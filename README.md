# terraform-applier

Heavily adapted from
[kube-applier](https://github.com/utilitywarehouse/kube-applier),
terraform-applier enables continuous deployment of Terraform code by applying
modules from a Git repository. Only 1 repository is supported for all the modules.

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
  path: dev/hello
  schedule: "00 */1 * * *"
  suspend: false
  planOnly: false
  pollInterval: 60
  runTimeout: 900
  delegateServiceAccountSecretRef: terraform-applier-delegate-token
  env:
    - name: AWS_REGION
      value: eu-west-1
    - name: "AWS_SECRET_ACCESS_KEY"
      valueFrom:
        secretKeyRef:
          name: hello-module-secrets
          key: AWS_SECRET_KEY
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

#### Delegate ServiceAccount

To minimize access required by controller on other namespaces, the concept of a
delegate ServiceAccount is introduced. When fetching secrets and configmaps for the Module, terraform-applier will use the credentials defined in the Secret referenced by
`delegateServiceAccountSecretRef`. This is a ServiceAccount in the same
namespace as the Module itself and should typically be given only `GET` access to only the secrets and configmaps referenced in module CRD.

#### ENV and VAR

The `envs` referenced in module will be set before the terraform run. this should not be used for any well known Terraform environment variables that are already covered in options. [more info](https://pkg.go.dev/github.com/hashicorp/terraform-exec@v0.18.1/tfexec#Terraform)

All referenced `vars` will be json encoded as key-value pair and written to temp file `*.auto.tfvars.json` in module's root folder. Terraform will load these vars during `plan` and `apply`.

#### Graceful shutdown

To make sure all terraform module run does complete in finite time `runTimeout` is added to the module spec.
default value is `900s` and MAX value is `1800s`. Terraform run `(init,plan and apply if required)` should finish in this time otherwise it will be forced shutdown.

If controller received TERM signal during a module run, then it will try and finish current stage of the run (either `init`, `plan` or `apply`) without the force shutdown. during this case it will not process next stage. eg. if TERM signal received during `plan` stage then 
it will not do `apply` even if drift is detected. 

Controller will force shutdown on current stage run if it takes more time then `TERMINATION_GRACE_PERIOD` set on controller.

### Controller config
#### Environment variables

- `REPO_PATH` - (default: `/src`) Absolute path to the directory containing the modules
  to be applied. The immediate subdirectories of this directory should contain
  the root modules which will be referenced by users in `module`.

- `LOG_LEVEL` - (default: `INFO`) `TRACE|DEBUG|INFO|WARN|ERROR`, case insensitive
- `MIN_INTERVAL_BETWEEN_RUNS` - (default: `60`) The minimum interval in seconds, user can set
  between 2 consecutive runs. This value defines the frequency of runs.
- `TERMINATION_GRACE_PERIOD` - (default: `60`) Termination grace period is the ime given to
  the running job to finish current run after 1st TERM signal is received. After this timeout runner will be forced to shutdown.
  Ideally this timeout should be just below the `terminationGracePeriodSeconds` set on controller pod.
- `TERRAFORM_PATH` - (default: `""`) The local path to a terraform
  binary to use.
- `TERRAFORM_VERSION` - (default: `""`) The version of terraform to
  use. The applier will install the requested release when it starts up. If you
  don't specify an explicit version, it will choose the latest available
  one. Ignored if `TERRAFORM_PATH` is set.

## Monitoring

### Metrics

terraform-applier exports Prometheus metrics. The metrics are hosted on the webserver at `/__/metrics`.

In addition to the Prometheus default metrics, the following custom metrics are included:

- `terraform_applier_module_apply_count` - (tags: `module`, `success`) A Counter for each module that has had an apply attempt over the lifetime of
  the application, incremented with each apply attempt and tagged with the result of the run (`success=true|false`)
- `terraform_applier_module_apply_duration_seconds` - (tags: `module`, `success`) A Summary that keeps track of the durations of each apply run for
  each module, tagged with the result of the run (`success=true|false`)
- `terraform_applier_module_apply_success` - (tags: `module`) A `Gauge` which
  tracks whether the last apply run for a module was successful.
- `terraform_applier_terraform_exit_code_count` - (tags: `module`, `command`, `exit_code`) A `Counter` for each exit code returned by executions of
  `terraform`, labelled with the command issued (`init`, `plan`,`apply`) and the exit code. It's worth noting that `plan` will
  return a code of `2` if there are changes to be made, which is not an error or a failure, so you may wish to account for this in your alerting.
