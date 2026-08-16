package merge

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zinntikumugai/kubeconfig-merge/internal/config"
)

// rke2Kubeconfig is what RKE2 writes: everything is called "default".
const rke2Kubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: default
  cluster:
    server: https://127.0.0.1:6443
    certificate-authority-data: ZHVtbXktY2E=
users:
- name: default
  user:
    client-certificate-data: ZHVtbXktY2VydA==
    client-key-data: ZHVtbXkta2V5
contexts:
- name: default
  context:
    cluster: default
    user: default
current-context: default
preferences: {}
`

// twoContextKubeconfig holds two contexts sharing nothing.
const twoContextKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: kubernetes
  cluster:
    server: https://127.0.0.1:6443
    certificate-authority-data: ZHVtbXktY2E=
- name: staging
  cluster:
    server: https://127.0.0.2:6443
    certificate-authority-data: ZHVtbXktY2Et
users:
- name: kubernetes-admin
  user:
    client-certificate-data: ZHVtbXktY2VydA==
    client-key-data: ZHVtbXkta2V5
- name: staging-admin
  user:
    token: dummy-token
contexts:
- name: kubernetes-admin@kubernetes
  context:
    cluster: kubernetes
    user: kubernetes-admin
- name: staging-admin@kubernetes
  context:
    cluster: staging
    user: staging-admin
preferences: {}
`

func setup(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// build loads kconfig.yaml from dir (validating it the way the CLI does) and
// runs Build.
func build(t *testing.T, dir string, flatten bool) (*Result, error) {
	t.Helper()
	cfg, _, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config.Validate: %v", err)
	}
	return Build(dir, cfg, flatten || cfg.Options.Flatten, slog.New(slog.DiscardHandler))
}

func mustBuild(t *testing.T, dir string, flatten bool) *Result {
	t.Helper()
	res, err := build(t, dir, flatten)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return res
}

// 1. single kubeconfig / single context, with cluster / user / context rename
// and server overwrite.
func TestBuildSingleContext(t *testing.T) {
	dir := setup(t, map[string]string{
		"merino.kconfig.yaml": rke2Kubeconfig,
		"kconfig.yaml": `
version: 1
sources:
  merino:
    contexts:
      - source: default
        profile: merino-prod
profiles:
  merino-prod:
    cluster: {name: cluster-merino, server: "https://172.16.1.100:6443"}
    user: {name: cluster-merino-admin}
    context: {name: cluster-merino-admin}
current-context: cluster-merino-admin
`,
	})

	res := mustBuild(t, dir, false)
	out := res.Config

	if len(out.Clusters) != 1 || len(out.AuthInfos) != 1 || len(out.Contexts) != 1 {
		t.Fatalf("got %d clusters, %d users, %d contexts; want 1 each", len(out.Clusters), len(out.AuthInfos), len(out.Contexts))
	}
	cluster, ok := out.Clusters["cluster-merino"]
	if !ok {
		t.Fatalf("cluster was not renamed: %v", keys(out.Clusters))
	}
	if cluster.Server != "https://172.16.1.100:6443" {
		t.Errorf("server = %q, want the overwritten one", cluster.Server)
	}
	if string(cluster.CertificateAuthorityData) != "dummy-ca" {
		t.Errorf("CA data was not carried over: %q", cluster.CertificateAuthorityData)
	}
	user, ok := out.AuthInfos["cluster-merino-admin"]
	if !ok {
		t.Fatalf("user was not renamed: %v", keys(out.AuthInfos))
	}
	if string(user.ClientKeyData) != "dummy-key" {
		t.Errorf("client key was not carried over")
	}
	kctx, ok := out.Contexts["cluster-merino-admin"]
	if !ok {
		t.Fatalf("context was not renamed: %v", keys(out.Contexts))
	}
	if kctx.Cluster != "cluster-merino" || kctx.AuthInfo != "cluster-merino-admin" {
		t.Errorf("context references %q/%q, want the renamed cluster/user", kctx.Cluster, kctx.AuthInfo)
	}
	if out.CurrentContext != "cluster-merino-admin" {
		t.Errorf("current-context = %q", out.CurrentContext)
	}

	if len(res.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(res.Entries))
	}
	want := Entry{
		SourceID: "merino", SourceFile: "merino.kconfig.yaml", SourceContext: "default",
		ProfileName: "merino-prod", ContextName: "cluster-merino-admin",
		ClusterName: "cluster-merino", UserName: "cluster-merino-admin",
		Server: "https://172.16.1.100:6443",
	}
	if res.Entries[0] != want {
		t.Errorf("entry = %+v\nwant       %+v", res.Entries[0], want)
	}
}

// 2. single kubeconfig / multiple contexts, in kconfig.yaml order.
func TestBuildMultipleContextsFromOneSource(t *testing.T) {
	dir := setup(t, map[string]string{
		"merino.kconfig.yaml": twoContextKubeconfig,
		"kconfig.yaml": `
version: 1
sources:
  merino:
    contexts:
      - source: kubernetes-admin@kubernetes
        profile: merino-prod
      - source: staging-admin@kubernetes
        profile: merino-stg
profiles:
  merino-prod:
    cluster: {name: cluster-merino, server: "https://172.16.1.100:6443"}
    user: {name: cluster-merino-admin}
    context: {name: cluster-merino-admin}
  merino-stg:
    cluster: {name: cluster-merino-stg, server: "https://172.16.1.101:6443"}
    user: {name: cluster-merino-stg-admin}
    context: {name: cluster-merino-stg-admin}
`,
	})

	res := mustBuild(t, dir, false)
	if len(res.Config.Contexts) != 2 {
		t.Fatalf("got %d contexts, want 2", len(res.Config.Contexts))
	}
	if got := res.Config.AuthInfos["cluster-merino-stg-admin"].Token; got != "dummy-token" {
		t.Errorf("token was not carried over: %q", got)
	}
	// Entries follow the order written in kconfig.yaml.
	if got := []string{res.Entries[0].ContextName, res.Entries[1].ContextName}; got[0] != "cluster-merino-admin" || got[1] != "cluster-merino-stg-admin" {
		t.Errorf("entries out of file order: %v", got)
	}
	// Nothing that was not selected leaks in.
	if _, ok := res.Config.Contexts["kubernetes-admin@kubernetes"]; ok {
		t.Error("source context name leaked into the output")
	}
}

// 3. multiple kubeconfigs, 20. .yml and .yaml inputs both work.
func TestBuildMultipleSources(t *testing.T) {
	dir := setup(t, map[string]string{
		"merino.kconfig.yaml": twoContextKubeconfig,
		"kikyo.kconfig.yml":   rke2Kubeconfig,
		"kconfig.yaml": `
version: 1
sources:
  merino:
    contexts:
      - source: kubernetes-admin@kubernetes
        profile: merino-prod
  kikyo:
    contexts:
      - source: default
        profile: kikyo-prod
profiles:
  merino-prod:
    cluster: {name: cluster-merino, server: "https://172.16.1.100:6443"}
    user: {name: cluster-merino-admin}
    context: {name: cluster-merino-admin}
  kikyo-prod:
    cluster: {name: cluster-kikyo, server: "https://172.16.2.100:6443"}
    user: {name: cluster-kikyo-admin}
    context: {name: cluster-kikyo-admin}
current-context: cluster-kikyo-admin
`,
	})

	res := mustBuild(t, dir, false)
	if len(res.Config.Contexts) != 2 {
		t.Fatalf("got %d contexts, want 2: %v", len(res.Config.Contexts), keys(res.Config.Contexts))
	}
	// Sources are processed in sorted order: kikyo before merino.
	if res.Entries[0].SourceID != "kikyo" || res.Entries[1].SourceID != "merino" {
		t.Errorf("sources not processed in sorted order: %q, %q", res.Entries[0].SourceID, res.Entries[1].SourceID)
	}
	if res.Entries[0].SourceFile != "kikyo.kconfig.yml" {
		t.Errorf("source file = %q, want the .yml one", res.Entries[0].SourceFile)
	}
}

// 7. an omitted server keeps the original one.
func TestBuildKeepsServerWhenNotOverridden(t *testing.T) {
	dir := setup(t, map[string]string{
		"merino.kconfig.yaml": rke2Kubeconfig,
		"kconfig.yaml": `
version: 1
sources:
  merino:
    contexts:
      - source: default
        profile: merino-prod
profiles:
  merino-prod:
    cluster: {name: cluster-merino}
    user: {name: cluster-merino-admin}
    context: {name: cluster-merino-admin}
`,
	})

	res := mustBuild(t, dir, false)
	if got := res.Config.Clusters["cluster-merino"].Server; got != "https://127.0.0.1:6443" {
		t.Errorf("server = %q, want the original one", got)
	}
	if got := res.Entries[0].Server; got != "https://127.0.0.1:6443" {
		t.Errorf("entry server = %q, want the original one", got)
	}
}

// 8, 9, 10, 12, 13, 14, 15, 21 and the missing-source-file case.
func TestBuildErrors(t *testing.T) {
	brokenClusterRef := strings.Replace(rke2Kubeconfig, "    cluster: default\n", "    cluster: nope\n", 1)
	brokenUserRef := strings.Replace(rke2Kubeconfig, "    user: default\n", "    user: nope\n", 1)

	tests := []struct {
		name    string
		files   map[string]string
		wantErr string
	}{
		{
			name: "source context not found",
			files: map[string]string{
				"merino.kconfig.yaml": rke2Kubeconfig,
				"kconfig.yaml": `
version: 1
sources:
  merino:
    contexts: [{source: foo, profile: p}]
profiles:
  p:
    cluster: {name: c}
    user: {name: u}
    context: {name: x}
`,
			},
			wantErr: `source "merino": context "foo" was not found`,
		},
		{
			name: "context references unknown cluster",
			files: map[string]string{
				"merino.kconfig.yaml": brokenClusterRef,
				"kconfig.yaml": `
version: 1
sources:
  merino:
    contexts: [{source: default, profile: p}]
profiles:
  p:
    cluster: {name: c}
    user: {name: u}
    context: {name: x}
`,
			},
			wantErr: `source "merino", context "default": cluster "nope" was not found`,
		},
		{
			name: "context references unknown user",
			files: map[string]string{
				"merino.kconfig.yaml": brokenUserRef,
				"kconfig.yaml": `
version: 1
sources:
  merino:
    contexts: [{source: default, profile: p}]
profiles:
  p:
    cluster: {name: c}
    user: {name: u}
    context: {name: x}
`,
			},
			wantErr: `source "merino", context "default": user "nope" was not found`,
		},
		{
			name: "duplicate output cluster name",
			files: map[string]string{
				"merino.kconfig.yaml": twoContextKubeconfig,
				"kconfig.yaml": `
version: 1
sources:
  merino:
    contexts:
      - {source: kubernetes-admin@kubernetes, profile: a}
      - {source: staging-admin@kubernetes, profile: b}
profiles:
  a:
    cluster: {name: same}
    user: {name: user-a}
    context: {name: ctx-a}
  b:
    cluster: {name: same}
    user: {name: user-b}
    context: {name: ctx-b}
`,
			},
			wantErr: `cluster name "same" is used twice`,
		},
		{
			name: "duplicate output user name",
			files: map[string]string{
				"merino.kconfig.yaml": twoContextKubeconfig,
				"kconfig.yaml": `
version: 1
sources:
  merino:
    contexts:
      - {source: kubernetes-admin@kubernetes, profile: a}
      - {source: staging-admin@kubernetes, profile: b}
profiles:
  a:
    cluster: {name: cluster-a}
    user: {name: same}
    context: {name: ctx-a}
  b:
    cluster: {name: cluster-b}
    user: {name: same}
    context: {name: ctx-b}
`,
			},
			wantErr: `user name "same" is used twice`,
		},
		{
			name: "duplicate output context name",
			files: map[string]string{
				"merino.kconfig.yaml": twoContextKubeconfig,
				"kconfig.yaml": `
version: 1
sources:
  merino:
    contexts:
      - {source: kubernetes-admin@kubernetes, profile: a}
      - {source: staging-admin@kubernetes, profile: b}
profiles:
  a:
    cluster: {name: cluster-a}
    user: {name: user-a}
    context: {name: same}
  b:
    cluster: {name: cluster-b}
    user: {name: user-b}
    context: {name: same}
`,
			},
			wantErr: `context name "same" is used twice`,
		},
		{
			name: "current-context not produced",
			files: map[string]string{
				"merino.kconfig.yaml": rke2Kubeconfig,
				"kconfig.yaml": `
version: 1
sources:
  merino:
    contexts: [{source: default, profile: p}]
profiles:
  p:
    cluster: {name: c}
    user: {name: u}
    context: {name: x}
current-context: nope
`,
			},
			wantErr: `current-context "nope" is not one of the merged contexts`,
		},
		{
			name: "ambiguous source file",
			files: map[string]string{
				"merino.kconfig.yaml": rke2Kubeconfig,
				"merino.kconfig.yml":  rke2Kubeconfig,
				"kconfig.yaml": `
version: 1
sources:
  merino:
    contexts: [{source: default, profile: p}]
profiles:
  p:
    cluster: {name: c}
    user: {name: u}
    context: {name: x}
`,
			},
			wantErr: `source "merino": both merino.kconfig.yaml and merino.kconfig.yml exist`,
		},
		{
			name: "source file missing",
			files: map[string]string{
				"kconfig.yaml": `
version: 1
sources:
  merino:
    contexts: [{source: default, profile: p}]
profiles:
  p:
    cluster: {name: c}
    user: {name: u}
    context: {name: x}
`,
			},
			wantErr: `source "merino": no merino.kconfig.yaml or merino.kconfig.yml found`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setup(t, tt.files)
			_, err := build(t, dir, false)
			if err == nil {
				t.Fatalf("got nil error, want one containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q\nwant it to contain %q", err, tt.wantErr)
			}
		})
	}
}

const fileRefKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: default
  cluster:
    server: https://127.0.0.1:6443
    certificate-authority: ./certs/ca.crt
users:
- name: default
  user:
    client-certificate: ./certs/client.crt
    client-key: ./certs/client.key
contexts:
- name: default
  context:
    cluster: default
    user: default
preferences: {}
`

const flattenKconfig = `
version: 1
sources:
  merino:
    contexts: [{source: default, profile: p}]
profiles:
  p:
    cluster: {name: cluster-merino}
    user: {name: cluster-merino-admin}
    context: {name: cluster-merino-admin}
`

func fileRefFiles() map[string]string {
	return map[string]string{
		"merino.kconfig.yaml": fileRefKubeconfig,
		"kconfig.yaml":        flattenKconfig,
		"certs/ca.crt":        "dummy-ca-pem",
		"certs/client.crt":    "dummy-client-pem",
		"certs/client.key":    "dummy-key-pem",
	}
}

// 18. flatten=false keeps file references, resolved to absolute paths so that
// they stay valid wherever the merged config is used from.
func TestBuildFlattenFalse(t *testing.T) {
	dir := setup(t, fileRefFiles())

	res := mustBuild(t, dir, false)
	cluster := res.Config.Clusters["cluster-merino"]
	if len(cluster.CertificateAuthorityData) != 0 {
		t.Error("CA was embedded even though flatten is off")
	}
	if want := filepath.Join(dir, "certs", "ca.crt"); cluster.CertificateAuthority != want {
		t.Errorf("certificate-authority = %q, want the absolute path %q", cluster.CertificateAuthority, want)
	}
	user := res.Config.AuthInfos["cluster-merino-admin"]
	if want := filepath.Join(dir, "certs", "client.key"); user.ClientKey != want {
		t.Errorf("client-key = %q, want the absolute path %q", user.ClientKey, want)
	}
}

// 19. flatten=true embeds the referenced files as data.
func TestBuildFlattenTrue(t *testing.T) {
	dir := setup(t, fileRefFiles())

	res := mustBuild(t, dir, true)
	cluster := res.Config.Clusters["cluster-merino"]
	if cluster.CertificateAuthority != "" {
		t.Errorf("certificate-authority = %q, want it replaced by data", cluster.CertificateAuthority)
	}
	if got := string(cluster.CertificateAuthorityData); got != "dummy-ca-pem" {
		t.Errorf("certificate-authority-data = %q", got)
	}
	user := res.Config.AuthInfos["cluster-merino-admin"]
	if user.ClientCertificate != "" || user.ClientKey != "" {
		t.Error("client cert/key paths survived flattening")
	}
	if got := string(user.ClientCertificateData); got != "dummy-client-pem" {
		t.Errorf("client-certificate-data = %q", got)
	}
	if got := string(user.ClientKeyData); got != "dummy-key-pem" {
		t.Errorf("client-key-data = %q", got)
	}
}

// options.flatten in kconfig.yaml is honoured on its own.
func TestBuildFlattenFromOptions(t *testing.T) {
	files := fileRefFiles()
	files["kconfig.yaml"] = "options:\n  flatten: true\n" + flattenKconfig
	dir := setup(t, files)

	res := mustBuild(t, dir, false)
	if len(res.Config.Clusters["cluster-merino"].CertificateAuthorityData) == 0 {
		t.Error("options.flatten: true was ignored")
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
