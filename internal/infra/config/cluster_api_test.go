package config

import (
	"os"
	"testing"
)

func TestClusterAPIConfigAndEnv(t *testing.T) {
	cfg := Defaults()
	if cfg.ClusterAPIKubeconfig != "" {
		t.Fatal("integration must be opt-in")
	}
	if err := ApplyYAML(&cfg, []byte("cluster_api:\n  kubeconfig: /etc/gomi/capi.yaml\n  context: management\n  namespace: team-a\n")); err != nil {
		t.Fatal(err)
	}
	if cfg.ClusterAPIKubeconfig != "/etc/gomi/capi.yaml" || cfg.ClusterAPIContext != "management" || cfg.ClusterAPINamespace != "team-a" {
		t.Fatalf("%+v", cfg)
	}
	t.Setenv("GOMI_CLUSTER_API_KUBECONFIG", "/env/capi.yaml")
	t.Setenv("GOMI_CLUSTER_API_CONTEXT", "env-management")
	t.Setenv("GOMI_CLUSTER_API_NAMESPACE", "team-b")
	ApplyEnv(&cfg)
	if cfg.ClusterAPIKubeconfig != "/env/capi.yaml" || cfg.ClusterAPIContext != "env-management" || cfg.ClusterAPINamespace != "team-b" {
		t.Fatal("environment overrides lost")
	}
}

func TestClusterAPIEmptyAndAbsentEnvOverrides(t *testing.T) {
	for _, present := range []bool{false, true} {
		name := "unset"
		if present {
			name = "explicit-empty"
		}
		t.Run(name, func(t *testing.T) {
			for _, key := range []string{"GOMI_CLUSTER_API_KUBECONFIG", "GOMI_CLUSTER_API_CONTEXT", "GOMI_CLUSTER_API_NAMESPACE"} {
				t.Setenv(key, "")
				if !present {
					if err := os.Unsetenv(key); err != nil {
						t.Fatal(err)
					}
				}
			}
			cfg := Defaults()
			if err := ApplyYAML(&cfg, []byte("cluster_api:\n  kubeconfig: /etc/gomi/capi.yaml\n  context: management\n  namespace: team-a\n")); err != nil {
				t.Fatal(err)
			}
			ApplyEnv(&cfg)
			wantPath, wantContext, wantNamespace := "/etc/gomi/capi.yaml", "management", "team-a"
			if present {
				wantPath, wantContext, wantNamespace = "", "", ""
			}
			if cfg.ClusterAPIKubeconfig != wantPath || cfg.ClusterAPIContext != wantContext || cfg.ClusterAPINamespace != wantNamespace {
				t.Fatalf("Cluster API override mismatch: %q %q %q", cfg.ClusterAPIKubeconfig, cfg.ClusterAPIContext, cfg.ClusterAPINamespace)
			}
		})
	}
}
