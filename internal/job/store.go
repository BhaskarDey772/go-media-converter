package job

import (
	"errors"
	"sync"
)

var ErrNotFound = errors.New("job not found")

type Store interface {
	Create(j Job) error
	Get(id string) (Job, error)
	Update(j Job) error
}

type MemStore struct {
	mu   sync.RWMutex
	jobs map[string]Job
}

func NewMemStore() *MemStore {
	return &MemStore{jobs: make(map[string]Job)}
}

func (s *MemStore) Create(j Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = j
	return nil
}

func (s *MemStore) Get(id string) (Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	return j, nil
}

func (s *MemStore) Update(j Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[j.ID]; !ok {
		return ErrNotFound
	}
	s.jobs[j.ID] = j
	return nil
}
