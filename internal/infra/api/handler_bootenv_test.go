package api_test

import (
	"net/http"
	"testing"
)

func TestBootEnvironmentsListStartsMissing(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/boot-environments", nil, env.token)
	requireStatus(t, rec, http.StatusOK)

	body := parseBody(t, rec)
	items := listValues(t, body)
	want := map[string]bool{
		"bootEnvironments/ubuntu-minimal-cloud-amd64": false,
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("expected boot environment object, got %T", raw)
		}
		if _, ok := want[item["name"].(string)]; ok {
			if item["phase"] != "missing" {
				t.Fatalf("expected %s to start missing, got %v", item["name"], item["phase"])
			}
			want[item["name"].(string)] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("expected %s boot environment", name)
		}
	}
}
