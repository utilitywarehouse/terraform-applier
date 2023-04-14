package controllers

import (
	"fmt"

	"github.com/hashicorp/go-hclog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type Filter struct {
	predicate.Funcs
	LabelSelectorKey   string
	LabelSelectorValue string
	Log                hclog.Logger
}

func (f Filter) Create(e event.CreateEvent) bool {
	if e.Object == nil {
		return false
	}
	return f.LabelSelectorFilter(e.Object)
}

func (f Filter) Delete(e event.DeleteEvent) bool {
	if !f.LabelSelectorFilter(e.Object) {
		return false
	}
	// Evaluates to false if the object has been confirmed deleted.
	return !e.DeleteStateUnknown
}

func (f Filter) Generic(e event.GenericEvent) bool {
	if e.Object == nil {
		return false
	}
	return f.LabelSelectorFilter(e.Object)
}

func (f Filter) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	if !f.LabelSelectorFilter(e.ObjectNew) {
		return false
	}

	// Ignore updates to CR status in which case metadata.Generation does not change
	if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
		return true
	}

	f.Log.Trace("skipping module update event", "module", fmt.Sprintf("%s/%s", e.ObjectNew.GetNamespace(), e.ObjectNew.GetName()))
	return false
}

func (f Filter) LabelSelectorFilter(object client.Object) bool {
	// allow all if selector Labels is not set
	if f.LabelSelectorKey == "" {
		return true
	}

	return object.GetLabels()[f.LabelSelectorKey] == f.LabelSelectorValue
}
