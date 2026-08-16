// Package merge extracts the selected contexts from the source kubeconfig
// files, applies the profile overrides and assembles one kubeconfig out of
// them.
//
// Nothing here touches the filesystem beyond reading the sources: the caller
// decides what to do with the result.
package merge

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/zinntikumugai/kubeconfig-merge/internal/config"
)

// Entry describes one merged context: where it came from and what it became.
type Entry struct {
	SourceID      string // e.g. "merino"
	SourceFile    string // base name, e.g. "merino.kconfig.yaml"
	SourceContext string // context name in the source kubeconfig
	ProfileName   string
	ContextName   string
	ClusterName   string
	UserName      string
	Server        string
}

// Result is the merged kubeconfig plus a human-readable account of it.
type Result struct {
	Config  *clientcmdapi.Config
	Entries []Entry
}

// Build merges the contexts selected by cfg out of the kubeconfig files in dir.
// cfg must have passed config.Validate. Nothing is written; on any error the
// merge is abandoned as a whole.
func Build(dir string, cfg *config.Config, flatten bool, log *slog.Logger) (*Result, error) {
	out := clientcmdapi.NewConfig()
	res := &Result{Config: out}

	for _, id := range cfg.SourceIDs() {
		path, err := findSourceFile(dir, id)
		if err != nil {
			return nil, err
		}
		log.Info("loading source", "source", id, "file", path)

		src, err := loadKubeconfig(path, flatten)
		if err != nil {
			return nil, fmt.Errorf("source %q: %w", id, err)
		}

		for _, ref := range cfg.Sources[id].Contexts {
			srcCtx, ok := src.Contexts[ref.Source]
			if !ok {
				return nil, fmt.Errorf("source %q: context %q was not found in %s", id, ref.Source, filepath.Base(path))
			}
			srcCluster, ok := src.Clusters[srcCtx.Cluster]
			if !ok {
				return nil, fmt.Errorf("source %q, context %q: cluster %q was not found in %s", id, ref.Source, srcCtx.Cluster, filepath.Base(path))
			}
			srcUser, ok := src.AuthInfos[srcCtx.AuthInfo]
			if !ok {
				return nil, fmt.Errorf("source %q, context %q: user %q was not found in %s", id, ref.Source, srcCtx.AuthInfo, filepath.Base(path))
			}
			log.Info("selecting context", "source", id, "context", ref.Source,
				"cluster", srcCtx.Cluster, "user", srcCtx.AuthInfo, "profile", ref.Profile)

			// Presence is guaranteed by config.Validate.
			profile := cfg.Profiles[ref.Profile]
			where := fmt.Sprintf("source %q, context %q", id, ref.Source)

			if _, dup := out.Clusters[profile.Cluster.Name]; dup {
				return nil, fmt.Errorf("%s: cluster name %q is used twice in the merged kubeconfig", where, profile.Cluster.Name)
			}
			if _, dup := out.AuthInfos[profile.User.Name]; dup {
				return nil, fmt.Errorf("%s: user name %q is used twice in the merged kubeconfig", where, profile.User.Name)
			}
			if _, dup := out.Contexts[profile.Context.Name]; dup {
				return nil, fmt.Errorf("%s: context name %q is used twice in the merged kubeconfig", where, profile.Context.Name)
			}

			cluster := srcCluster.DeepCopy()
			if profile.Cluster.Server != "" {
				cluster.Server = profile.Cluster.Server
			}
			out.Clusters[profile.Cluster.Name] = cluster
			out.AuthInfos[profile.User.Name] = srcUser.DeepCopy()

			newCtx := srcCtx.DeepCopy()
			newCtx.Cluster = profile.Cluster.Name
			newCtx.AuthInfo = profile.User.Name
			out.Contexts[profile.Context.Name] = newCtx

			log.Info("applied profile", "profile", ref.Profile, "context", profile.Context.Name,
				"cluster", profile.Cluster.Name, "user", profile.User.Name, "server", cluster.Server)

			res.Entries = append(res.Entries, Entry{
				SourceID:      id,
				SourceFile:    filepath.Base(path),
				SourceContext: ref.Source,
				ProfileName:   ref.Profile,
				ContextName:   profile.Context.Name,
				ClusterName:   profile.Cluster.Name,
				UserName:      profile.User.Name,
				Server:        cluster.Server,
			})
		}
	}

	if cc := cfg.CurrentContext; cc != "" {
		if _, ok := out.Contexts[cc]; !ok {
			return nil, fmt.Errorf("current-context %q is not one of the merged contexts", cc)
		}
		out.CurrentContext = cc
	}

	if err := clientcmd.Validate(*out); err != nil {
		return nil, fmt.Errorf("the merged kubeconfig is not valid: %w", err)
	}
	return res, nil
}

// findSourceFile locates <id>.kconfig.yaml or <id>.kconfig.yml in dir. Having
// both is an error: which one was meant is anybody's guess.
func findSourceFile(dir, id string) (string, error) {
	yamlName, ymlName := id+".kconfig.yaml", id+".kconfig.yml"
	yamlPath, ymlPath := filepath.Join(dir, yamlName), filepath.Join(dir, ymlName)
	hasYAML, err := exists(yamlPath)
	if err != nil {
		return "", err
	}
	hasYML, err := exists(ymlPath)
	if err != nil {
		return "", err
	}

	switch {
	case hasYAML && hasYML:
		return "", fmt.Errorf("source %q: both %s and %s exist in %s: remove one", id, yamlName, ymlName, dir)
	case hasYAML:
		return yamlPath, nil
	case hasYML:
		return ymlPath, nil
	default:
		return "", fmt.Errorf("source %q: no %s or %s found in %s", id, yamlName, ymlName, dir)
	}
}

// loadKubeconfig reads one source kubeconfig. Relative file references are
// resolved against the source file's own directory before anything else, so
// they keep working from wherever the merged config is later used; with flatten
// they are read in and embedded as data instead.
func loadKubeconfig(path string, flatten bool) (*clientcmdapi.Config, error) {
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return nil, err
	}
	if err := clientcmd.ResolveLocalPaths(cfg); err != nil {
		return nil, err
	}
	if flatten {
		if err := clientcmdapi.FlattenConfig(cfg); err != nil {
			return nil, err
		}
	}
	return cfg, nil
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
