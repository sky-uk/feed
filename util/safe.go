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

// SafeError is a thread safe error
type SafeError struct {
	val error
	m   sync.Mutex
}

// Get the value inside the SafeError
func (s *SafeError) Get() error {
	s.m.Lock()
	defer s.m.Unlock()
	return s.val
}

// Set the value inside the SafeError
func (s *SafeError) Set(newVal error) {
	s.m.Lock()
	s.val = newVal
	s.m.Unlock()
}
