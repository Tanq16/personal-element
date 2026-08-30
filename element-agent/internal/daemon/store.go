package daemon

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"encoding/json/jsontext"
	"encoding/json/v2"
)

const (
	instructionsFile = "AGENTS.md"
	skillsDir        = ".agents/skills"
	ResultFile       = ".result"
)

var claudeLinks = map[string]string{
	"CLAUDE.md":      instructionsFile,
	".claude/skills": "../" + skillsDir,
}

var ErrNoAgent = errors.New("agent is not configured on this machine")

type Agent struct {
	Name                  string   `json:"name"`
	Claim                 string   `json:"claim"`
	Argv                  []string `json:"argv,omitempty"`
	AllowMessageRetrieval bool     `json:"allow_message_retrieval,omitempty"`
}

func (a Agent) Registered() bool { return len(a.Argv) > 0 }

type Store struct{ root string }

func NewStore(root string) *Store { return &Store{root: root} }

func (s *Store) Dir(name string) string {
	return filepath.Join(s.root, "agents", name)
}

func (s *Store) List() ([]Agent, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "agents"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var agents []Agent
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agent, err := s.Load(entry.Name())
		if errors.Is(err, ErrNoAgent) {
			continue
		}
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	slices.SortFunc(agents, func(a, b Agent) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return agents, nil
}

func (s *Store) Load(name string) (Agent, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir(name), "agent.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Agent{}, ErrNoAgent
	}
	if err != nil {
		return Agent{}, err
	}
	var agent Agent
	if err := json.Unmarshal(data, &agent); err != nil {
		return Agent{}, err
	}
	return agent, nil
}

func (s *Store) Save(agent Agent) error {
	dir := s.Dir(agent.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(agent, jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "agent.json"), data, 0o600)
}

func (s *Store) Scaffold(agent Agent) error {
	dir := s.Dir(agent.Name)
	if err := os.MkdirAll(filepath.Join(dir, skillsDir), 0o700); err != nil {
		return err
	}
	if err := s.Save(agent); err != nil {
		return err
	}

	instructions := filepath.Join(dir, instructionsFile)
	if _, err := os.Stat(instructions); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(instructions, []byte("# "+agent.Name+"\n"), 0o600); err != nil {
			return err
		}
	}

	for name, target := range claudeLinks {
		link := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
			return err
		}
		if err := os.Symlink(target, link); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	return nil
}
