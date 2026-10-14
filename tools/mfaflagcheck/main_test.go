package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFlagsNonDemoManifests(t *testing.T) {
	root := t.TempDir()
	bad := map[string]string{
		"deploy/compose/docker-compose.vps.yml":           "    environment:\n      AG_MFA_DISABLED: ${AG_MFA_DISABLED:-1}\n",
		"deploy/compose/.env":                             "AG_MFA_DISABLED=true\n",
		"prod.env":                                        "AG_DEPLOY_ENV=demo\n",
		"deploy/k8s/charts/agrigreen/values-prod.yaml":    "env:\n  - name: AG_MFA_DISABLED\n    value: \"1\"\n",
		"deploy/k8s/charts/agrigreen/values-staging.yaml": "extraEnv:\n  AG_MFA_DISABLED: \"1\"\n",
		".github/workflows/ci.yml":                        "env:\n  AG_DEPLOY_ENV: demo\n",
	}
	for rel, body := range bad {
		write(t, root, rel, body)
	}
	ok := map[string]string{
		"deploy/compose/docker-compose.yml":     "      AG_MFA_DISABLED: ${AG_MFA_DISABLED:-0}\n      AG_DEPLOY_ENV: ${AG_DEPLOY_ENV:-}\n",
		"deploy/compose/.env.example":           "# AG_DEPLOY_ENV=demo\n# AG_MFA_DISABLED=1\nAG_MFA_DISABLED=0\n",
		".github/workflows/trien-khai-demo.yml": "AG_MFA_DISABLED=1\nAG_DEPLOY_ENV=demo\n",
		"deploy/demo/.env":                      "AG_MFA_DISABLED=1\n",
		"docs/x.yml":                            "AG_MFA_DISABLED: 1\n",
		"internal/x/config.yaml":                "AG_MFA_DISABLED: 1\n", // ngoài phạm vi
	}
	for rel, body := range ok {
		write(t, root, rel, body)
	}
	got, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, "\n")
	for rel := range bad {
		if !strings.Contains(joined, rel+":") {
			t.Errorf("không bắt %s\n%s", rel, joined)
		}
	}
	for rel := range ok {
		if strings.Contains(joined, rel+":") {
			t.Errorf("báo nhầm %s\n%s", rel, joined)
		}
	}
}

// Repo hiện tại phải sạch (cùng điều kiện với bước CI).
func TestRepoClean(t *testing.T) {
	got, err := Check(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 0 {
		t.Fatalf("vi phạm:\n%s", strings.Join(got, "\n"))
	}
}
