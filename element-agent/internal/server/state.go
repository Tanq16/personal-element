package server

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"encoding/json/jsontext"
	"encoding/json/v2"
)

const (
	StatusReserved = "reserved"
	StatusServing  = "serving"
)

type Record struct {
	Status string `json:"status"`
	Claim  string `json:"claim"`
}

type stateFile struct {
	Agents map[string]Record `json:"agents"`
}

type State struct {
	path   string
	mu     sync.RWMutex
	agents map[string]Record
}

func LoadState(path string) (*State, error) {
	s := &State{path: path, agents: map[string]Record{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var file stateFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.Agents != nil {
		s.agents = file.Agents
	}
	return s, nil
}

func (s *State) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Sorted(maps.Keys(s.agents))
}

func (s *State) Serving() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var names []string
	for name, record := range s.agents {
		if record.Status == StatusServing {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func (s *State) Lookup(name string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.agents[name]
	return record, ok
}

func (s *State) Reserve(name, claim string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[name] = Record{Status: StatusReserved, Claim: claim}
	return s.save()
}

func (s *State) SetServing(name string, serving bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.agents[name]
	if !ok {
		return nil
	}
	record.Status = StatusReserved
	if serving {
		record.Status = StatusServing
	}
	s.agents[name] = record
	return s.save()
}

func (s *State) Release(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.agents, name)
	return s.save()
}

func (s *State) save() error {
	data, err := json.Marshal(stateFile{Agents: s.agents}, jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	temp := s.path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, s.path)
}
