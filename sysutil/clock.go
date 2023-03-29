package sysutil

import "time"

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

// Used for testing purpose
type FakeClock struct {
	T time.Time
}

func (c *FakeClock) Now() time.Time { return c.T }
