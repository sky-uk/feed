package util

import (
	"sync"
)

// SafeBool is a thread safe boolean
type SafeBool struct {
	val bool
	sync.Mutex
}

// Get the value inside the SafeBool
func (s *SafeBool) Get() bool {
	s.Lock()
	defer s.Unlock()
	return s.val
}

// Set the value inside the SafeBool
func (s *SafeBool) Set(newVal bool) {
	s.Lock()
	s.val = newVal
	s.Unlock()
}

// SafeError is a thread safe error
type SafeError struct {
	val error
	sync.Mutex
}

// Get the value inside the SafeError
func (s *SafeError) Get() error {
	s.Lock()
	defer s.Unlock()
	return s.val
}

// Set the value inside the SafeError
func (s *SafeError) Set(newVal error) {
	s.Lock()
	s.val = newVal
	s.Unlock()
}

// SafeInt is a thread safe int
type SafeInt struct {
	val int
	sync.Mutex
}

// Get the value inside SafeInt
func (s *SafeInt) Get() int {
	s.Lock()
	defer s.Unlock()
	return s.val
}

// Add a value to the SafeInt
func (s *SafeInt) Add(addend int) int {
	s.Lock()
	defer s.Unlock()
	s.val += addend
	return s.val
}

// Set the value of the SafeInt
func (s *SafeInt) Set(val int) int {
	s.Lock()
	defer s.Unlock()
	s.val = val
	return s.val
}
