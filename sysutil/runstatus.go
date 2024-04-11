package sysutil

import "sync"

// RunStatus is the Map with Lock so its safe for concurrent use
// sync.Map is not used as it doesn't have Len() function and normal map with
// lock will do for our limited use case
type RunStatus struct {
	*sync.RWMutex
	status map[string]interface{}
}

func NewRunStatus() *RunStatus {
	return &RunStatus{
		&sync.RWMutex{},
		make(map[string]interface{}),
	}
}

// Delete deletes the value for a key.
func (rs *RunStatus) Delete(key string) {
	rs.Lock()
	defer rs.Unlock()

	delete(rs.status, key)
}

// Len returns current length of the Map
func (rs *RunStatus) Len(key string) int {
	rs.RLock()
	defer rs.RUnlock()

	return len(rs.status)
}

// Load returns the value stored in the map for a key, or nil if no value is present.
// The ok result indicates whether value was found in the map.
func (rs *RunStatus) Load(key string) (interface{}, bool) {
	rs.RLock()
	defer rs.RUnlock()

	v, ok := rs.status[key]
	return v, ok
}

// Store sets the value for a key.
func (rs *RunStatus) Store(key string, value interface{}) {
	rs.Lock()
	defer rs.Unlock()

	rs.status[key] = value
}
