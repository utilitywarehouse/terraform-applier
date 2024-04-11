package webserver

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"github.com/utilitywarehouse/terraform-applier/webserver/oidc"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const trace = slog.Level(-8)

//go:embed static
var staticFiles embed.FS

//go:embed templates/status.html
var statusHTML string

var log *slog.Logger

// WebServer struct
type WebServer struct {
	ListenAddress string
	Authenticator *oidc.Authenticator
	ClusterClt    client.Client
	KubeClient    kubernetes.Interface
	RunStatus     *sysutil.RunStatus
	Log           *slog.Logger
}

// StatusPageHandler implements the http.Handler interface and serves a status page with info about the most recent applier run.
type StatusPageHandler struct {
	Template      *template.Template
	Authenticator *oidc.Authenticator
	ClusterClt    client.Client
	Log           *slog.Logger
}

// ServeHTTP populates the status page template with data and serves it when there is a request.
func (s *StatusPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Log.Log(r.Context(), trace, "Applier status request")

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
	s.Log.Log(r.Context(), trace, "Request completed successfully")
}

// ForceRunHandler implements the http.Handle interface and serves an API
// endpoint for forcing a new run.
type ForceRunHandler struct {
	Authenticator *oidc.Authenticator
	ClusterClt    client.Client
	KubeClt       kubernetes.Interface
	RunStatus     *sysutil.RunStatus
	Log           *slog.Logger
}

// ServeHTTP handles requests for forcing a run by attempting to add to the
// runQueue, and writes a response including the result and a relevant message.
func (f *ForceRunHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.Log.Debug("force run requested")
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

	var user *oidc.UserInfo
	var err error

	// authentication
	// check if user logged in
	if f.Authenticator != nil {
		user, err = f.Authenticator.UserInfo(r.Context(), r)
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
		data.Message = fmt.Sprintf("cannot find module '%s'", namespacedName)
		f.Log.Error(data.Message, "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// authorisation
	// check if user has access
	if f.Authenticator != nil {
		// this should not happen but just in case
		if user == nil {
			data.Result = "error"
			data.Message = "logged in user's details not found"
			f.Log.Error(data.Message, "module", namespacedName)
			w.WriteHeader(http.StatusForbidden)
			return
		}

		// just to give useful error message to user
		if len(module.Spec.RBAC) == 0 {
			data.Result = "error"
			data.Message = fmt.Sprintf("force run is not allowed because RBAC is not set on module %s", namespacedName)
			f.Log.Error("RBAC is not set", "module", namespacedName)
			w.WriteHeader(http.StatusForbidden)
			return
		}

		// check if logged in user allowed to do force run on the module
		hasAccess := tfaplv1beta1.CanForceRun(user.Email, user.Groups, &module)
		if !hasAccess {
			data.Result = "error"
			data.Message = fmt.Sprintf("user %s is not allowed to force run module %s", user.Email, namespacedName)
			f.Log.Error("force run denied", "module", namespacedName, "user", user.Email)
			w.WriteHeader(http.StatusForbidden)
			return
		}

		f.Log.Info("requesting force run...", "module", namespacedName, "user", user.Email)
	}

	// make sure module is not already running
	_, ok := f.RunStatus.Load(namespacedName.String())
	if ok {
		data.Result = "error"
		data.Message = fmt.Sprintf("module %s is currently running", namespacedName)
		f.Log.Error("force run rejected as module is already running", "module", namespacedName)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	reqType := tfaplv1beta1.ForcedPlan
	if payload["planOnly"] == "false" {
		reqType = tfaplv1beta1.ForcedApply
	}

	req := module.NewRunRequest(reqType)

	err = sysutil.EnsureRequest(r.Context(), f.ClusterClt, req)
	switch {
	case err == nil:
		data.Result = "success"
		data.Message = "Run queued"
		f.Log.Info("force run requested", "module", namespacedName, "req", req)
		w.WriteHeader(http.StatusOK)
		return
	case errors.Is(err, tfaplv1beta1.ErrRunRequestExist):
		data.Result = "error"
		data.Message = "Unable to request run as another request is pending"
		f.Log.Error("unable to request force run", "module", namespacedName, "err", err)
		w.WriteHeader(http.StatusOK)
		return
	default:
		data.Result = "error"
		data.Message = "internal error"
		f.Log.Error("unable to request force run", "module", namespacedName, "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
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
		ws.RunStatus,
		ws.Log,
	}
	m.PathPrefix("/static/").Handler(http.FileServer(http.FS(staticFiles)))
	m.PathPrefix("/api/v1/forceRun").Handler(forceRunHandler)
	m.PathPrefix("/").Handler(statusPageHandler)

	return http.ListenAndServe(ws.ListenAddress, m)
}
