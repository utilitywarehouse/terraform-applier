package sysutil

import (
	"sync"
	"time"
)

// ClockInterface allows for mocking out the functionality of the standard time library when testing.
type ClockInterface interface {
	Now() time.Time
}

// Clock implements ClockInterface with the standard time library functions.
type Clock struct{}

// Now returns current time
func (c *Clock) Now() time.Time {
	return time.Now()
}

// FakeClock is used for testing purpose. Safe for concurrent use via SetTime/Now.
type FakeClock struct {
	mu sync.RWMutex
	T  time.Time
}

// SetTime updates the fake clock's time. Safe for concurrent use with Now.
func (c *FakeClock) SetTime(t time.Time) {
	c.mu.Lock()
	c.T = t
	c.mu.Unlock()
}

func (c *FakeClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.T
}
