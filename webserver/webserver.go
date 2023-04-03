package webserver

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/hashicorp/go-hclog"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/runner"
	"github.com/utilitywarehouse/terraform-applier/webserver/oidc"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
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
	Authenticator *oidc.Authenticator
	ClusterClt    client.Client
	KubeClient    kubernetes.Interface
	RunQueue      chan<- runner.Request
	Log           hclog.Logger
}

// StatusPageHandler implements the http.Handler interface and serves a status page with info about the most recent applier run.
type StatusPageHandler struct {
	Template      *template.Template
	Authenticator *oidc.Authenticator
	ClusterClt    client.Client
	Log           hclog.Logger
}

// ServeHTTP populates the status page template with data and serves it when there is a request.
func (s *StatusPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Log.Debug("Applier status request")

	if s.Authenticator != nil {
		_, err := s.Authenticator.Authenticate(r.Context(), w, r)
		if errors.Is(err, oidc.ErrRedirectRequired) {
			return
		}
		if err != nil {
			http.Error(w, "Error: Authentication failed", http.StatusInternalServerError)
			s.Log.Error("Authentication failed", "error", err)
			return
		}
	}

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

// ForceRunHandler implements the http.Handle interface and serves an API
// endpoint for forcing a new run.
type ForceRunHandler struct {
	Authenticator *oidc.Authenticator
	ClusterClt    client.Client
	KubeClt       kubernetes.Interface
	RunQueue      chan<- runner.Request
	Log           hclog.Logger
}

// ServeHTTP handles requests for forcing a run by attempting to add to the
// runQueue, and writes a response including the result and a relevant message.
func (f *ForceRunHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.Log.Info("Force run requested")
	var data struct {
		Result  string `json:"result"`
		Message string `json:"message"`
	}

	defer func() {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}()

	if r.Method != "POST" {
		data.Result = "error"
		data.Message = "must be a POST request"
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var (
		userEmail string
		err       error
	)
	if f.Authenticator != nil {
		userEmail, err = f.Authenticator.UserEmail(r.Context(), r)
		if err != nil {
			data.Result = "error"
			data.Message = "not authenticated"
			f.Log.Error(data.Message, "error", err)
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}

	payload := map[string]string{}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		data.Result = "error"
		data.Message = "unable to read body"
		f.Log.Error(data.Message, "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		data.Result = "error"
		data.Message = "unable to parse body"
		f.Log.Error(data.Message, "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if payload["namespace"] == "" || payload["module"] == "" {
		data.Result = "error"
		data.Message = "namespace and module name required"
		f.Log.Error(data.Message)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	namespacedName := types.NamespacedName{
		Namespace: payload["namespace"],
		Name:      payload["module"],
	}
	var module tfaplv1beta1.Module
	err = f.ClusterClt.Get(r.Context(), namespacedName, &module)
	if err != nil {
		data.Result = "error"
		data.Message = fmt.Sprintf("cannot find Waybills in namespace '%s' with name '%s'", payload["namespace"], payload["module"])
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if f.Authenticator != nil {
		// if the user can patch the Waybill, they are allowed to force a run
		hasAccess, err := hasAccess(r.Context(), f.KubeClt, &module, userEmail, "patch")
		if !hasAccess {
			data.Result = "error"
			data.Message = fmt.Sprintf("user %s is not allowed to force a run module on %s/%s", userEmail, module.Namespace, module.Name)
			if err != nil {
				f.Log.Error(data.Message, "error", err)
			}
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}

	f.RunQueue <- runner.Request{NamespacedName: namespacedName, Type: tfaplv1beta1.ForcedRun}

	data.Result = "success"
	data.Message = "Run queued"
	w.WriteHeader(http.StatusOK)
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
		ws.Authenticator,
		ws.ClusterClt,
		ws.Log,
	}
	forceRunHandler := &ForceRunHandler{
		ws.Authenticator,
		ws.ClusterClt,
		ws.KubeClient,
		ws.RunQueue,
		ws.Log,
	}
	m.PathPrefix("/static/").Handler(http.FileServer(http.FS(staticFiles)))
	m.PathPrefix("/api/v1/forceRun").Handler(forceRunHandler)
	m.PathPrefix("/").Handler(statusPageHandler)

	return http.ListenAndServe(ws.ListenAddress, m)
}
