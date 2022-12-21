package webserver

import (
	"embed"
	"encoding/json"
	"net/http"
	"text/template"

	"github.com/gorilla/mux"
	"github.com/utilitywarehouse/terraform-applier/log"
	"github.com/utilitywarehouse/terraform-applier/run"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
)

//go:embed static
var staticFiles embed.FS

//go:embed templates/status.html
var statusHTML string

// WebServer struct
type WebServer struct {
	ListenAddress string
	Clock         sysutil.ClockInterface
	RunQueue      chan<- bool
	RunResults    <-chan run.Result
	Errors        chan<- error
}

// StatusPageHandler implements the http.Handler interface and serves a status page with info about the most recent applier run.
type StatusPageHandler struct {
	Template *template.Template
	Data     interface{}
	Clock    sysutil.ClockInterface
}

// ServeHTTP populates the status page template with data and serves it when there is a request.
func (s *StatusPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Debug("Applier status request")
	if s.Template == nil {
		http.Error(w, "Error: Unable to load HTML template", http.StatusInternalServerError)
		log.Error("Request failed, no template found")
		return
	}
	if err := s.Template.ExecuteTemplate(w, "index", s.Data); err != nil {
		http.Error(w, "Error: Unable to load HTML template", http.StatusInternalServerError)
		log.Error("Request failed: %v", err)
		return
	}
	log.Debug("Request completed successfully")
}

// ForceRunHandler implements the http.Handle interface and serves an API endpoint for forcing a new run.
type ForceRunHandler struct {
	RunQueue chan<- bool
}

// ServeHTTP handles requests for forcing a run by attempting to add to the runQueue, and writes a response including the result and a relevant message.
func (f *ForceRunHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Info("Force run requested")
	var data struct {
		Result  string `json:"result"`
		Message string `json:"message"`
	}

	switch r.Method {
	case "POST":
		select {
		case f.RunQueue <- true:
			log.Info("Run queued")
		default:
			log.Info("Run queue is already full")
		}
		data.Result = "success"
		data.Message = "Run queued, will begin upon completion of current run."
		w.WriteHeader(http.StatusOK)
	default:
		data.Result = "error"
		data.Message = "Error: force rejected, must be a POST request."
		w.WriteHeader(http.StatusBadRequest)
		log.Error("%v", data.Message)
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	json.NewEncoder(w).Encode(data)
}

// Start starts the webserver using the given port, and sets up handlers for:
// 1. Status page
// 2. Metrics
// 3. Static content
// 4. Endpoint for forcing a run
func (ws *WebServer) Start() {
	log.Info("Launching webserver")
	lastRun := &run.Result{}

	template, err := sysutil.CreateTemplate(statusHTML)
	if err != nil {
		ws.Errors <- err
		return
	}

	m := mux.NewRouter()
	addStatusEndpoints(m)
	statusPageHandler := &StatusPageHandler{
		template,
		lastRun,
		ws.Clock,
	}
	m.PathPrefix("/static/").Handler(http.FileServer(http.FS(staticFiles)))
	forceRunHandler := &ForceRunHandler{
		ws.RunQueue,
	}
	m.PathPrefix("/api/v1/forceRun").Handler(forceRunHandler)
	m.PathPrefix("/").Handler(statusPageHandler)

	go func() {
		for result := range ws.RunResults {
			*lastRun = result
		}
	}()

	err = http.ListenAndServe(ws.ListenAddress, m)
	ws.Errors <- err
}
