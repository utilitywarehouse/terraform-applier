package webserver

import (
	"context"
	"embed"
	"html/template"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/hashicorp/go-hclog"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed static
var staticFiles embed.FS

//go:embed templates/status.html
var statusHTML string

var log hclog.Logger

// WebServer struct
type WebServer struct {
	ListenAddress string
	ClusterClt    client.Client
	Clock         sysutil.ClockInterface
	RunQueue      chan<- ctrl.Request
	Log           hclog.Logger
}

// StatusPageHandler implements the http.Handler interface and serves a status page with info about the most recent applier run.
type StatusPageHandler struct {
	Template   *template.Template
	ClusterClt client.Client
	Log        hclog.Logger
	Clock      sysutil.ClockInterface
}

// ServeHTTP populates the status page template with data and serves it when there is a request.
func (s *StatusPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Log.Debug("Applier status request")
	if s.Template == nil {
		http.Error(w, "Error: Unable to load HTML template", http.StatusInternalServerError)
		log.Error("Request failed, no template found")
		return
	}

	modules, err := listModules(r.Context(), s.ClusterClt)
	if err != nil {
		http.Error(w, "Error: Unable to get modules", http.StatusInternalServerError)
		log.Error("Request failed: %v", err)
		return
	}

	result := createNamespaceMap(modules)

	if err := s.Template.ExecuteTemplate(w, "index", result); err != nil {
		http.Error(w, "Error: Unable to execute HTML template", http.StatusInternalServerError)
		log.Error("Request failed: %v", err)
		return
	}
	s.Log.Debug("Request completed successfully")
}

// Start starts the webserver using the given port, and sets up handlers for:
// 1. Status page
// 2. Static content
func (ws *WebServer) Start(ctx context.Context) error {
	ws.Log.Info("Launching webserver")

	template, err := createTemplate(statusHTML)
	if err != nil {
		return err
	}

	m := mux.NewRouter()
	addStatusEndpoints(m)
	statusPageHandler := &StatusPageHandler{
		template,
		ws.ClusterClt,
		ws.Log,
		ws.Clock,
	}
	m.PathPrefix("/static/").Handler(http.FileServer(http.FS(staticFiles)))
	m.PathPrefix("/").Handler(statusPageHandler)

	return http.ListenAndServe(ws.ListenAddress, m)
}
