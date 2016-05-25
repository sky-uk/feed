package ingress

import "sync"

type safeBool struct {
	val bool
	m   sync.Mutex
}

func (s *safeBool) get() bool {
	s.m.Lock()
	defer s.m.Unlock()
	return s.val
}

func (s *safeBool) set(newVal bool) {
	s.m.Lock()
	s.val = newVal
	s.m.Unlock()
}
