# terraform-applier

Heavily adapted from
[kube-applier](https://github.com/utilitywarehouse/kube-applier),
terraform-applier enables continuous deployment of Terraform code by applying
modules from a Git repository or local directory.

## Usage

### Environment variables

#### Required

- `MODULES_PATH` - (string) Absolute path to the directory containing the modules
  to be applied. The immediate subdirectories of this directory should contain
  the root modules you wish to apply.
  - If this path is a git repository then the modules will be reapplied when
    there is a change in the repo.

#### Optional

- `DIFF_URL_FORMAT` - (string) (default: `""`) Should be a URL for a hosted remote repo that supports linking to a commit hash. Replace the commit
  hash portion with `%s` so it can be filled in by terraform-applier (e.g. `https://github.com/kubernetes/kubernetes/commit/%s`) .
- `DRY_RUN` - (bool) (default: `false`) If `true`, terraform-applier will stop after running `plan`, whether there are changes to be made or not
- `FULL_RUN_INTERVAL_SECONDS` - (int) (default: `3600`) Number of seconds between automatic full runs . Set to `0` to disable
- `LISTEN_ADDRESS` - (string) (default: `:8080`) The address the applier webserver will listen on
- `LOG_LEVEL` - (string) (default: `INFO`) `TRACE|DEBUG|INFO|WARN|ERROR|FATAL`, case insensitive
- `POLL_INTERVAL_SECONDS` - (int) (default: `5`) Number of seconds to wait between each check for new commits to the repo
- `MODULES_PATH_FILTERS` - (string) (default: `""`) A comma separated list of sub directories to be applied. Supports [shell file name patterns](https://golang.org/pkg/path/filepath/#Match).
- `TERRAFORM_PATH` - (string) (default: `""`) The local path to a terraform
  binary to use.
- `TERRAFORM_VERSION` - (string) (default: `""`) The version of terraform to
  use. The applier will install the requested release when it starts up. If you
  don't specify an explicit version, it will choose the latest available
  one. Ignored if `TERRAFORM_PATH` is set.

#### Variables used by terraform resources

You can also provide environment variables for use by terraform providers (such as AWS_ACCESS_KEY_ID) or variables for use in your
code (TF_VAR_your_variable_name). This is useful for providing sensitive values that you don't want to save in version control or
variables that are only available in your Kube environment

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
