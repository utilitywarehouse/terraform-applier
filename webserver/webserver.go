package webserver

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/mux"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/runner"
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
	RunQueue      chan<- runner.Request
	RunStatus     *sync.Map
	Log           *slog.Logger
}

type EventsPageHandler struct {
	Authenticator *oidc.Authenticator
	ClusterClt    client.Client
	KubeClt       kubernetes.Interface
	RunQueue      chan<- runner.Request
	RunStatus     *sync.Map
	Log           hclog.Logger
}

type Comment struct {
	Body string `json:"body"`
}

func (s *EventsPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}
	
	// Parse JSON payload
	var payload map[string]interface{}
	err = json.Unmarshal(body, &payload)
	if err != nil {
		http.Error(w, "Error decoding JSON payload", http.StatusBadRequest)
		return
	}
 
	// Collect necessary values from the payload
	repoName, ok := payload["repository"].(map[string]interface{})["full_name"].(string)
	if !ok {
		http.Error(w, "Error retrieving repository name", http.StatusBadRequest)
		return
	}
	prNumberFloat, ok := payload["pull_request"].(map[string]interface{})["number"].(float64)
	if !ok {
		http.Error(w, "Error retrieving pull request number", http.StatusBadRequest)
		return
	}
	prNumber := fmt.Sprint(prNumberFloat)
	// branchName, ok
	_, ok = payload["pull_request"].(map[string]interface{})["head"].(map[string]interface{})["ref"].(string)
	if !ok {
		http.Error(w, "Error retrieving branch name", http.StatusBadRequest)
		return
	}

  s.PostToGitHub(repoName, prNumber)

}


func (s *EventsPageHandler) PostToGitHub(repoName, prNumber string) {
  username:="DTLP"
	token:=os.Getenv("GITHUB_TOKEN")

	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/%s/comments", repoName, prNumber)

	comment := Comment{
		Body: "Hello from terraform-applier",
	}

	// Marshal the comment object to JSON
	commentJSON, err := json.Marshal(comment)
	if err != nil {
		fmt.Println("Error marshalling comment to JSON:", err)
	}

	// Create a new HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(commentJSON))
	if err != nil {
	  fmt.Println("Error creating HTTP request:", err)
	  return
  } 

  // Set headers
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(username, token)

	// Send the HTTP request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending HTTP request:", err)
		return
	}
	defer resp.Body.Close()

	// Check the response status
	if resp.StatusCode != http.StatusCreated {
    fmt.Println("Error:", resp.Status)
		return
	}

	fmt.Println("Comment posted successfully.")
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
	RunQueue      chan<- runner.Request
	RunStatus     *sync.Map
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
	isPlanOnly := true
	if payload["planOnly"] == "false" {
		isPlanOnly = false
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

		f.Log.Info("force run triggered", "module", namespacedName, "isPlanOnly", isPlanOnly, "user", user.Email)
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

	req := runner.Request{
		NamespacedName: namespacedName,
		Type:           tfaplv1beta1.ForcedPlan,
		PlanOnly:       isPlanOnly,
	}
	if !isPlanOnly {
		req.Type = tfaplv1beta1.ForcedApply
	}

	f.RunQueue <- req

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
	eventsPageHandler := &EventsPageHandler{
		ws.Authenticator,
		ws.ClusterClt,
		ws.KubeClient,
		ws.RunQueue,
		ws.RunStatus,
		ws.Log,
	}
	forceRunHandler := &ForceRunHandler{
		ws.Authenticator,
		ws.ClusterClt,
		ws.KubeClient,
		ws.RunQueue,
		ws.RunStatus,
		ws.Log,
	}
	m.PathPrefix("/static/").Handler(http.FileServer(http.FS(staticFiles)))
	m.PathPrefix("/api/v1/forceRun").Handler(forceRunHandler)
	m.PathPrefix("/events").Handler(eventsPageHandler)
	m.PathPrefix("/").Handler(statusPageHandler)

	return http.ListenAndServe(ws.ListenAddress, m)
}
