// Package state persists what the app configs cannot hold: installed
// commits, image ids, config hashes and installer flags.
package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const currentVersion = 1

type Repo struct {
	Commit      string    `yaml:"commit"`
	InstalledAt time.Time `yaml:"installed_at"`
}

type Image struct {
	Tag string `yaml:"tag"`
	ID  string `yaml:"id"`
}

type ConfigHash struct {
	SHA256 string `yaml:"sha256"`
}

type WorkflowSkills struct {
	Commit string            `yaml:"commit"`
	Files  map[string]string `yaml:"files"`
}

type Migration struct {
	DoneAt time.Time `yaml:"done_at"`
	From   []string  `yaml:"from"`
}

type State struct {
	Version        int                   `yaml:"version"`
	OS             string                `yaml:"os"`
	ServiceManager string                `yaml:"service_manager"`
	Docker         bool                  `yaml:"docker"`
	Repos          map[string]Repo       `yaml:"repos"`
	Images         map[string]Image      `yaml:"images"`
	Configs        map[string]ConfigHash `yaml:"configs"`
	WorkflowSkills WorkflowSkills        `yaml:"workflow_skills"`
	Migration      *Migration            `yaml:"migration,omitempty"`
}

func New() *State {
	return &State{
		Version:        currentVersion,
		Repos:          map[string]Repo{},
		Images:         map[string]Image{},
		Configs:        map[string]ConfigHash{},
		WorkflowSkills: WorkflowSkills{Files: map[string]string{}},
	}
}

func Load(path string) (*State, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("read state: %w", err)
	}

	s := New()
	if err := yaml.Unmarshal(data, s); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}

	// Unmarshal replaces the maps with nil when the file has empty sections.
	if s.Repos == nil {
		s.Repos = map[string]Repo{}
	}

	if s.Images == nil {
		s.Images = map[string]Image{}
	}

	if s.Configs == nil {
		s.Configs = map[string]ConfigHash{}
	}

	if s.WorkflowSkills.Files == nil {
		s.WorkflowSkills.Files = map[string]string{}
	}

	return s, true, nil
}

func (s *State) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}

	return nil
}

func (s *State) Installed() bool {
	return s.Repos["contextmatrix"].Commit != ""
}
