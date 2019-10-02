package webserver

import (
	"fmt"
	"net/http/pprof"

	"github.com/gorilla/mux"
	"github.com/utilitywarehouse/go-operational/op"
)

const appName = "terraform-applier"
const appDescription = "enables continuous deployment of Terraform modules"

func addStatusEndpoints(m *mux.Router) *mux.Router {
	m.PathPrefix("/__/").Handler(op.NewHandler(op.NewStatus(appName, appDescription).
		AddOwner("system", "#infra").
		AddLink("readme", fmt.Sprintf("https://github.com/utilitywarehouse/%s/blob/master/README.md", appName)).
		ReadyAlways()))
	m.PathPrefix("/debug/pprof/cmdline").HandlerFunc(pprof.Cmdline)
	m.PathPrefix("/debug/pprof/profile").HandlerFunc(pprof.Profile)
	m.PathPrefix("/debug/pprof/symbol").HandlerFunc(pprof.Symbol)
	m.PathPrefix("/debug/pprof/trace").HandlerFunc(pprof.Trace)
	m.PathPrefix("/debug/pprof/").HandlerFunc(pprof.Index)
	return m
}
