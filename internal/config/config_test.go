package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const validYAML = `
version: 1

options:
  flatten: true

sources:
  merino:
    contexts:
      - source: kubernetes-admin@kubernetes
        profile: merino-prod
      - source: staging-admin@kubernetes
        profile: merino-stg
  kikyo:
    contexts:
      - source: default
        profile: kikyo-prod

profiles:
  merino-prod:
    cluster:
      name: cluster-merino
      server: https://172.16.1.100:6443
    user:
      name: cluster-merino-admin
    context:
      name: cluster-merino-admin
  merino-stg:
    cluster:
      name: cluster-merino-stg
    user:
      name: cluster-merino-stg-admin
    context:
      name: cluster-merino-stg-admin
  kikyo-prod:
    cluster:
      name: cluster-kikyo
      server: https://172.16.2.100:6443
    user:
      name: cluster-kikyo-admin
    context:
      name: cluster-kikyo-admin

current-context: cluster-merino-admin
`

func writeConfig(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadValid(t *testing.T) {
	for _, name := range []string{"kconfig.yaml", "kconfig.yml"} {
		t.Run(name, func(t *testing.T) {
			dir := writeConfig(t, name, validYAML)

			cfg, path, err := Load(dir)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if want := filepath.Join(dir, name); path != want {
				t.Errorf("path = %q, want %q", path, want)
			}
			if cfg.Version != 1 {
				t.Errorf("Version = %d, want 1", cfg.Version)
			}
			if !cfg.Options.Flatten {
				t.Error("Options.Flatten = false, want true")
			}
			if cfg.CurrentContext != "cluster-merino-admin" {
				t.Errorf("CurrentContext = %q", cfg.CurrentContext)
			}
			if got, want := cfg.SourceIDs(), []string{"kikyo", "merino"}; !reflect.DeepEqual(got, want) {
				t.Errorf("SourceIDs() = %v, want %v (sorted)", got, want)
			}
			want := []ContextRef{
				{Source: "kubernetes-admin@kubernetes", Profile: "merino-prod"},
				{Source: "staging-admin@kubernetes", Profile: "merino-stg"},
			}
			if got := cfg.Sources["merino"].Contexts; !reflect.DeepEqual(got, want) {
				t.Errorf("merino contexts = %+v, want %+v (in file order)", got, want)
			}
			p := cfg.Profiles["merino-prod"]
			if p.Cluster.Name != "cluster-merino" || p.Cluster.Server != "https://172.16.1.100:6443" ||
				p.User.Name != "cluster-merino-admin" || p.Context.Name != "cluster-merino-admin" {
				t.Errorf("profile merino-prod = %+v", p)
			}
			if s := cfg.Profiles["merino-stg"].Cluster.Server; s != "" {
				t.Errorf("omitted server = %q, want empty", s)
			}
			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate: %v", err)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	dir := writeConfig(t, "kconfig.yaml", `
version: 1
sources:
  a:
    contexts:
      - source: default
        profile: p
profiles:
  p:
    cluster: {name: c}
    user: {name: u}
    context: {name: x}
`)
	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Options.Flatten {
		t.Error("Options.Flatten defaults to true, want false")
	}
	if cfg.CurrentContext != "" {
		t.Errorf("CurrentContext = %q, want empty", cfg.CurrentContext)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestLoadAmbiguous(t *testing.T) {
	dir := writeConfig(t, "kconfig.yaml", validYAML)
	if err := os.WriteFile(filepath.Join(dir, "kconfig.yml"), []byte(validYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := Load(dir)
	assertErrContains(t, err, "both kconfig.yaml and kconfig.yml")
}

func TestLoadMissing(t *testing.T) {
	_, _, err := Load(t.TempDir())
	assertErrContains(t, err, "no kconfig.yaml or kconfig.yml found")
}

func TestLoadRejectsUnknownField(t *testing.T) {
	dir := writeConfig(t, "kconfig.yaml", `
version: 1
sources:
  a:
    contexts:
      - source: default
        profil: p
profiles:
  p:
    cluster: {name: c}
    user: {name: u}
    context: {name: x}
`)
	_, _, err := Load(dir)
	assertErrContains(t, err, "profil")
}

func TestLoadRejectsDuplicateKey(t *testing.T) {
	dir := writeConfig(t, "kconfig.yaml", `
version: 1
sources:
  a:
    contexts:
      - source: default
        profile: p
profiles:
  p:
    cluster: {name: c}
    user: {name: u}
    context: {name: x}
  p:
    cluster: {name: c2}
    user: {name: u2}
    context: {name: x2}
`)
	_, _, err := Load(dir)
	assertErrContains(t, err, "already set")
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "unsupported version",
			yaml: `
version: 2
sources:
  a: {contexts: [{source: default, profile: p}]}
profiles:
  p: {cluster: {name: c}, user: {name: u}, context: {name: x}}
`,
			wantErr: `unsupported version 2`,
		},
		{
			name: "missing profile",
			yaml: `
version: 1
sources:
  merino: {contexts: [{source: default, profile: merino-prod}]}
profiles:
  other: {cluster: {name: c}, user: {name: u}, context: {name: x}}
`,
			wantErr: `source "merino", context "default": profile "merino-prod" was not found`,
		},
		{
			name: "empty cluster name",
			yaml: `
version: 1
sources:
  a: {contexts: [{source: default, profile: p}]}
profiles:
  p: {cluster: {server: https://x:6443}, user: {name: u}, context: {name: x}}
`,
			wantErr: `profile "p": cluster.name is required`,
		},
		{
			name: "empty user name",
			yaml: `
version: 1
sources:
  a: {contexts: [{source: default, profile: p}]}
profiles:
  p: {cluster: {name: c}, user: {}, context: {name: x}}
`,
			wantErr: `profile "p": user.name is required`,
		},
		{
			name: "empty context name",
			yaml: `
version: 1
sources:
  a: {contexts: [{source: default, profile: p}]}
profiles:
  p: {cluster: {name: c}, user: {name: u}, context: {}}
`,
			wantErr: `profile "p": context.name is required`,
		},
		{
			name: "empty source context name",
			yaml: `
version: 1
sources:
  a: {contexts: [{profile: p}]}
profiles:
  p: {cluster: {name: c}, user: {name: u}, context: {name: x}}
`,
			wantErr: `source "a": contexts[0].source is required`,
		},
		{
			name: "source without contexts",
			yaml: `
version: 1
sources:
  a: {}
profiles:
  p: {cluster: {name: c}, user: {name: u}, context: {name: x}}
`,
			wantErr: `source "a": no contexts defined`,
		},
		{
			name:    "no sources",
			yaml:    "version: 1\n",
			wantErr: `no sources defined`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeConfig(t, "kconfig.yaml", tt.yaml)
			cfg, _, err := Load(dir)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			assertErrContains(t, cfg.Validate(), tt.wantErr)
		})
	}
}

func assertErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want one containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err, want)
	}
}
