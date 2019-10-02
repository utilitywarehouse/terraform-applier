# terraform-applier

Heavily adapted from [kube-applier](https://github.com/utilitywarehouse/kube-applier), terraform-applier enables continuous deployment of Terraform code by applying modules from a
Git repository.

## Usage

### Environment variables

#### Required

- `REPO_PATH` - (string) Absolute path to the directory containing the modules to be applied. It must be a Git repository or a path within
  one. The immediate subdirectories of this directory should contain the root modules you wish to apply.

#### Optional

- `DIFF_URL_FORMAT` - (string) (default: `""`) Should be a URL for a hosted remote repo that supports linking to a commit hash. Replace the commit
  hash portion with `%s` so it can be filled in by terraform-applier (e.g. `https://github.com/kubernetes/kubernetes/commit/%s`) .
- `DRY_RUN` - (bool) (default: `false`) If `true`, terraform-applier will stop after running `plan`, whether there are changes to be made or not
- `FULL_RUN_INTERVAL_SECONDS` - (int) (default: `3600`) Number of seconds between automatic full runs . Set to `0` to disable
- `INIT_ARGS` - (string) (default: `""`) A comma separated list of arguments to be passed to the `init` command. This is primarily useful for
  configuring backend options that are omitted from the code. If you include a `%s` in the string, terraform-applier will replace
  it with the basename of the module being applied. This can be used to configure the name of the state file
  - For instance, for a module with the path `/src/modules/vpc`, an `INIT_ARGS` value of `-backend-config=key=prod-%s` would be
    formatted as `-backend-config=key=prod-vpc` and could be used, in this example, to write state to an S3 object with the key
    `prod-vpc`.
- `LISTEN_ADDRESS` - (string) (default: `:8080`) The address the applier webserver will listen on
- `LOG_LEVEL` - (string) (default: `INFO`) `TRACE|DEBUG|INFO|WARN|ERROR|FATAL`, case insensitive
- `POLL_INTERVAL_SECONDS` - (int) (default: `5`) Number of seconds to wait between each check for new commits to the repo
- `REPO_PATH_FILTERS` - (string) (default: `""`) A comma separated list of sub directories to be applied. Supports [shell file name patterns](https://golang.org/pkg/path/filepath/#Match).

## Monitoring

### Metrics

terraform-applier exports Prometheus metrics. The metrics are hosted on the webserver at `/__/metrics`.

In addition to the Prometheus default metrics, the following custom metrics are included:

- `module_apply_count` - (tags: `module`, `success`) A Counter for each module that has had an apply attempt over the lifetime of
  the application, incremented with each apply attempt and tagged with the result of the run (`success=true|false`)
- `module_apply_duration_seconds` - (tags: `module`, `success`) A Summary that keeps track of the durations of each apply run for
  each module, tagged with the result of the run (`success=true|false`)
- `terraform_exit_code_count` - (tags: `module`, `command`, `exit_code`) A `Counter` for each exit code returned by executions of
  `terraform`, labelled with the command issued (`init`, `plan`,`apply`) and the exit code. It's worth noting that `plan` will
  return a code of `2` if there are changes to be made, which is not an error or a failure, so you may wish to account for this in your alerting.
