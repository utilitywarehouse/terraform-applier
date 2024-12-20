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

//go:embed templates/module.html
var moduleHTML string

// WebServer struct
type WebServer struct {
	ListenAddress string
	Authenticator *oidc.Authenticator
	ClusterClt    client.Client
	KubeClient    kubernetes.Interface
	Redis         sysutil.RedisInterface
	RunStatus     *sysutil.RunStatus
	Log           *slog.Logger
}

// StatusPageHandler implements the http.Handler interface and serves a status page with info about the most recent applier run.
type StatusPageHandler struct {
	Template      *template.Template
	Authenticator *oidc.Authenticator
	ClusterClt    client.Client
	Redis         sysutil.RedisInterface
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
			http.Error(w, "Authentication failed", http.StatusInternalServerError)
			s.Log.Error("Authentication failed", "error", err)
			return
		}
	}

	if s.Template == nil {
		http.Error(w, "Unable to load HTML template", http.StatusInternalServerError)
		s.Log.Error("Request failed, no template found")
		return
	}

	modules, err := listModules(r.Context(), s.ClusterClt)
	if err != nil {
		http.Error(w, "unable to get modules", http.StatusInternalServerError)
		s.Log.Error("Request failed", "err", err)
		return
	}

	result := createNamespaceMap(modules)

	if err := s.Template.ExecuteTemplate(w, "index", result); err != nil {
		http.Error(w, "Unable to execute HTML template", http.StatusInternalServerError)
		s.Log.Error("Request failed", "err", err)
		return
	}
	s.Log.Log(r.Context(), trace, "Request completed successfully")
}

// ModulePageHandler implements the http.Handler interface and serves a module info page with run outputs.
type ModulePageHandler struct {
	Template      *template.Template
	Authenticator *oidc.Authenticator
	ClusterClt    client.Client
	Redis         sysutil.RedisInterface
	Log           *slog.Logger
}

// ServeHTTP populates the status page template with data and serves it when there is a request.
func (m *ModulePageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m.Authenticator != nil {
		_, err := m.Authenticator.Authenticate(r.Context(), w, r)
		if errors.Is(err, oidc.ErrRedirectRequired) {
			return
		}
		if err != nil {
			http.Error(w, "Authentication failed", http.StatusInternalServerError)
			m.Log.Error("Authentication failed", "error", err)
			return
		}
	}

	if m.Template == nil {
		http.Error(w, "Unable to load HTML template", http.StatusInternalServerError)
		m.Log.Error("Request failed, no template found")
		return
	}

	payload, err := parseBody(r.Body)
	if err != nil {
		m.Log.Error("error parsing request", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	namespacedName := types.NamespacedName{
		Namespace: payload["namespace"],
		Name:      payload["module"],
	}

	module, err := moduleWithRunsInfo(r.Context(), m.ClusterClt, m.Redis, namespacedName)
	if err != nil {
		http.Error(w, "unable to get modules", http.StatusInternalServerError)
		m.Log.Error("unable to get modules", "err", err)
		return
	}

	if err := m.Template.ExecuteTemplate(w, "module", module); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		m.Log.Error("Unable to execute HTML template", "err", err)
		return
	}
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

	if r.Method != "POST" {
		http.Error(w, "must be a POST request", http.StatusBadRequest)
		return
	}

	var user *oidc.UserInfo
	var err error

	// authentication
	// check if user logged in
	if f.Authenticator != nil {
		user, err = f.Authenticator.UserInfo(r.Context(), r)
		if err != nil {
			f.Log.Error("not authenticated", "error", err)
			http.Error(w, "not authenticated", http.StatusForbidden)
			return
		}
	}

	payload, err := parseBody(r.Body)
	if err != nil {
		f.Log.Error("error parsing request", "error", err)
		http.Error(w, "error parsing request", http.StatusBadRequest)
		return
	}

	namespacedName := types.NamespacedName{
		Namespace: payload["namespace"],
		Name:      payload["module"],
	}

	var module tfaplv1beta1.Module
	err = f.ClusterClt.Get(r.Context(), namespacedName, &module)
	if err != nil {
		message := fmt.Sprintf("cannot find module '%s'", namespacedName)
		f.Log.Error(message, "error", err)
		http.Error(w, message, http.StatusBadRequest)
		return
	}

	// authorisation
	// check if user has access
	if f.Authenticator != nil {
		// this should not happen but just in case
		if user == nil {
			f.Log.Error("logged in user's details not found", "module", namespacedName)
			http.Error(w, "logged in user's details not found", http.StatusForbidden)
			return
		}

		// just to give useful error message to user
		if len(module.Spec.RBAC) == 0 {
			f.Log.Error("RBAC is not set", "module", namespacedName)
			http.Error(w, "force run is not allowed because RBAC is not set on module", http.StatusForbidden)
			return
		}

		// check if logged in user allowed to do force run on the module
		hasAccess := tfaplv1beta1.CanForceRun(user.Email, user.Groups, &module)
		if !hasAccess {
			f.Log.Error("force run denied", "module", namespacedName, "user", user.Email)
			http.Error(w,
				fmt.Sprintf("user %s is not allowed to force run module", user.Email),
				http.StatusForbidden)
			return
		}

		f.Log.Info("requesting force run...", "module", namespacedName, "user", user.Email)
	}

	// make sure module is not already running
	_, ok := f.RunStatus.Load(namespacedName.String())
	if ok {
		f.Log.Error("force run rejected as module is already running", "module", namespacedName)
		http.Error(w, "module is currently running", http.StatusBadRequest)
		return
	}

	reqType := tfaplv1beta1.ForcedPlan
	if payload["planOnly"] == "false" {
		reqType = tfaplv1beta1.ForcedApply
	}

	req := module.NewRunRequest(reqType)

	err = sysutil.EnsureRequest(r.Context(), f.ClusterClt, module.NamespacedName(), req)
	switch {
	case err == nil:
		f.Log.Info("force run requested", "module", namespacedName, "req", req)
		fmt.Fprint(w, "Run queued")
		return
	case errors.Is(err, tfaplv1beta1.ErrRunRequestExist):
		f.Log.Error("unable to request force run", "module", namespacedName, "err", err)
		http.Error(w,
			"Unable to request run as another request is pending",
			http.StatusConflict)
		return
	default:
		f.Log.Error("unable to request force run", "module", namespacedName, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
}

func parseBody(respBody io.ReadCloser) (map[string]string, error) {
	payload := map[string]string{}

	body, err := io.ReadAll(respBody)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	if payload["namespace"] == "" || payload["module"] == "" {
		return nil, fmt.Errorf("namespace and module name required")
	}

	return payload, nil
}

// Start starts the webserver using the given port, and sets up handlers for:
// 1. Status page
// 2. Static content
func (ws *WebServer) Start(ctx context.Context) error {
	ws.Log.Info("Launching webserver")

	statusTempt, err := createTemplate(statusHTML)
	if err != nil {
		return err
	}
	moduleTempt, err := createTemplate(moduleHTML)
	if err != nil {
		return err
	}

	m := mux.NewRouter()
	addStatusEndpoints(m)
	statusPageHandler := &StatusPageHandler{
		statusTempt,
		ws.Authenticator,
		ws.ClusterClt,
		ws.Redis,
		ws.Log,
	}
	modulePageHandler := &ModulePageHandler{
		moduleTempt,
		ws.Authenticator,
		ws.ClusterClt,
		ws.Redis,
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
	m.PathPrefix("/module").Handler(modulePageHandler)
	m.PathPrefix("/").Handler(statusPageHandler)

	return http.ListenAndServe(ws.ListenAddress, m)
}
