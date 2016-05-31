package util

import "sync"

// SafeBool is a thread safe boolean
type SafeBool struct {
	val bool
	m   sync.Mutex
}

// Get the value inside the SafeBool
func (s *SafeBool) Get() bool {
	s.m.Lock()
	defer s.m.Unlock()
	return s.val
}

// Set the value inside the SafeBool
func (s *SafeBool) Set(newVal bool) {
	s.m.Lock()
	s.val = newVal
	s.m.Unlock()
}
