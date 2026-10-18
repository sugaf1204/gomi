package config

import "testing"

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
