// Package config reads and validates kconfig.yaml, the file describing which
// contexts to take from which kubeconfig and how to rename them.
package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"sigs.k8s.io/yaml"
)

// Version is the only kconfig.yaml schema version this tool understands.
const Version = 1

// Config is the whole kconfig.yaml document.
type Config struct {
	Version        int                `json:"version"`
	Options        Options            `json:"options"`
	Sources        map[string]Source  `json:"sources"`
	Profiles       map[string]Profile `json:"profiles"`
	CurrentContext string             `json:"current-context"`
}

// Options holds the global switches.
type Options struct {
	Flatten bool `json:"flatten"`
}

// Source lists the contexts to take from one <id>.kconfig.yaml file.
type Source struct {
	Contexts []ContextRef `json:"contexts"`
}

// ContextRef selects one context of a source kubeconfig and the profile to
// apply to it.
type ContextRef struct {
	Source  string `json:"source"`
	Profile string `json:"profile"`
}

// Profile is the set of overrides applied to one selected context.
type Profile struct {
	Cluster ClusterOverride `json:"cluster"`
	User    NameOverride    `json:"user"`
	Context NameOverride    `json:"context"`
}

// ClusterOverride renames a cluster and optionally rewrites its server.
type ClusterOverride struct {
	Name   string `json:"name"`
	Server string `json:"server"`
}

// NameOverride renames a user or a context.
type NameOverride struct {
	Name string `json:"name"`
}

// Load finds kconfig.yaml (or kconfig.yml) in dir and parses it. It returns the
// parsed config and the path it was read from. Unknown or duplicated fields are
// rejected rather than silently ignored.
func Load(dir string) (*Config, string, error) {
	yamlPath := filepath.Join(dir, "kconfig.yaml")
	ymlPath := filepath.Join(dir, "kconfig.yml")
	hasYAML, err := exists(yamlPath)
	if err != nil {
		return nil, "", err
	}
	hasYML, err := exists(ymlPath)
	if err != nil {
		return nil, "", err
	}

	var path string
	switch {
	case hasYAML && hasYML:
		return nil, "", fmt.Errorf("both kconfig.yaml and kconfig.yml exist in %s: remove one", dir)
	case hasYAML:
		path = yamlPath
	case hasYML:
		path = ymlPath
	default:
		return nil, "", fmt.Errorf("no kconfig.yaml or kconfig.yml found in %s", dir)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var cfg Config
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, path, nil
}

// SourceIDs returns the source IDs in a stable (sorted) order.
func (c *Config) SourceIDs() []string {
	return slices.Sorted(maps.Keys(c.Sources))
}

// Validate checks everything that can be checked without reading any
// kubeconfig: schema version, profile references and required names.
func (c *Config) Validate() error {
	if c.Version != Version {
		return fmt.Errorf("unsupported version %d (this tool understands version %d)", c.Version, Version)
	}
	if len(c.Sources) == 0 {
		return errors.New("no sources defined")
	}

	for _, id := range c.SourceIDs() {
		src := c.Sources[id]
		if len(src.Contexts) == 0 {
			return fmt.Errorf("source %q: no contexts defined", id)
		}
		for i, ref := range src.Contexts {
			if ref.Source == "" {
				return fmt.Errorf("source %q: contexts[%d].source is required", id, i)
			}
			if ref.Profile == "" {
				return fmt.Errorf("source %q, context %q: profile is required", id, ref.Source)
			}
			if _, ok := c.Profiles[ref.Profile]; !ok {
				return fmt.Errorf("source %q, context %q: profile %q was not found", id, ref.Source, ref.Profile)
			}
		}
	}

	for _, name := range slices.Sorted(maps.Keys(c.Profiles)) {
		p := c.Profiles[name]
		switch {
		case p.Cluster.Name == "":
			return fmt.Errorf("profile %q: cluster.name is required", name)
		case p.User.Name == "":
			return fmt.Errorf("profile %q: user.name is required", name)
		case p.Context.Name == "":
			return fmt.Errorf("profile %q: context.name is required", name)
		}
	}
	return nil
}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
