package main

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/terraform"

	"github.com/utilitywarehouse/terraform-applier/log"
	"github.com/utilitywarehouse/terraform-applier/metrics"
	"github.com/utilitywarehouse/terraform-applier/run"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"github.com/utilitywarehouse/terraform-applier/webserver"
)

var (
	diffURLFormat   = os.Getenv("DIFF_URL_FORMAT")
	dryRun          = os.Getenv("DRY_RUN")
	fullRunInterval = os.Getenv("FULL_RUN_INTERVAL_SECONDS")
	initArgs        = os.Getenv("INIT_ARGS")
	listenAddress   = os.Getenv("LISTEN_ADDRESS")
	logLevel        = os.Getenv("LOG_LEVEL")
	pollInterval    = os.Getenv("POLL_INTERVAL_SECONDS")
	repoPath        = os.Getenv("REPO_PATH")
	repoPathFilters = os.Getenv("REPO_PATH_FILTERS")
)

func validate() {
	if repoPath == "" {
		log.Fatal("Need to export REPO_PATH")
	}

	if listenAddress == "" {
		listenAddress = ":8080"
	}

	if pollInterval == "" {
		pollInterval = "5"
	} else {
		_, err := strconv.Atoi(pollInterval)
		if err != nil {
			log.Fatal("POLL_INTERVAL_SECONDS must be an int")
		}
	}

	if fullRunInterval == "" {
		fullRunInterval = "3600"
	} else {
		_, err := strconv.Atoi(fullRunInterval)
		if err != nil {
			log.Fatal("FULL_RUN_INTERVAL_SECONDS must be an int")
		}
	}

	if diffURLFormat != "" && !strings.Contains(diffURLFormat, "%s") {
		log.Fatal("Invalid DIFF_URL_FORMAT, must contain %q: %v\n", "%s", diffURLFormat)
	}

	if dryRun == "" {
		dryRun = "false"
	} else {
		_, err := strconv.ParseBool(dryRun)
		if err != nil {
			log.Fatal("DRY_RUN must be a boolean")
		}
	}

	if logLevel == "" {
		logLevel = "INFO"
	}
}

func main() {
	log.Level = log.LevelFromString(logLevel)

	validate()

	clock := &sysutil.Clock{}

	metrics := &metrics.Prometheus{}
	metrics.Init()

	// Webserver and scheduler send run requests to runQueue channel, runner receives the requests and initiates runs.
	// Only 1 pending request may sit in the queue at a time.
	runQueue := make(chan bool, 1)

	// Runner sends run results to runResults channel, webserver receives the results and displays them.
	// Limit of 5 is arbitrary - there is significant delay between sends, and receives are handled near instantaneously.
	runResults := make(chan run.Result, 5)

	// Runner, webserver, and scheduler all send fatal errors to errors channel, and main() exits upon receiving an error.
	// No limit needed, as a single fatal error will exit the program anyway.
	errors := make(chan error)

	// Terraform client
	client := &terraform.Client{
		Metrics: metrics,
	}

	dr, _ := strconv.ParseBool(dryRun)
	applier := &run.Applier{
		Clock:           clock,
		DryRun:          dr,
		Errors:          errors,
		InitArgs:        initArgs,
		Metrics:         metrics,
		TerraformClient: client,
	}

	gitUtil := &git.Util{
		RepoPath: repoPath,
	}

	var repoPathFiltersSlice []string
	if repoPathFilters != "" {
		repoPathFiltersSlice = strings.Split(repoPathFilters, ",")
	}
	runner := &run.Runner{
		RepoPath:        repoPath,
		RepoPathFilters: repoPathFiltersSlice,
		Applier:         applier,
		DiffURLFormat:   diffURLFormat,
		GitUtil:         gitUtil,
		Metrics:         metrics,
		Clock:           clock,
		RunQueue:        runQueue,
		RunResults:      runResults,
		Errors:          errors,
	}

	pi, _ := strconv.Atoi(pollInterval)
	fi, _ := strconv.Atoi(fullRunInterval)
	scheduler := &run.Scheduler{
		FullRunInterval: time.Duration(fi) * time.Second,
		GitUtil:         gitUtil,
		PollInterval:    time.Duration(pi) * time.Second,
		RepoPathFilters: repoPathFiltersSlice,
		RunQueue:        runQueue,
		Errors:          errors,
	}

	webserver := &webserver.WebServer{
		ListenAddress: listenAddress,
		Clock:         clock,
		RunQueue:      runQueue,
		RunResults:    runResults,
		Errors:        errors,
	}

	go scheduler.Start()
	go runner.Start()
	go webserver.Start()

	err := <-errors
	log.Fatal("Fatal error, exiting: %v", err)

}
