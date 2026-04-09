package devcontainer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStripJSONC_LineComments(t *testing.T) {
	input := []byte(`{
  // This is a comment
  "name": "test"
}`)
	got := string(StripJSONC(input))
	if contains(got, "//") {
		t.Errorf("line comment not stripped: %s", got)
	}
	if !contains(got, `"name"`) {
		t.Errorf("content lost: %s", got)
	}
}

func TestStripJSONC_BlockComments(t *testing.T) {
	input := []byte(`{
  /* block comment */
  "name": "test"
}`)
	got := string(StripJSONC(input))
	if contains(got, "/*") || contains(got, "*/") {
		t.Errorf("block comment not stripped: %s", got)
	}
	if !contains(got, `"name"`) {
		t.Errorf("content lost: %s", got)
	}
}

func TestStripJSONC_PreservesStrings(t *testing.T) {
	input := []byte(`{"url": "https://example.com // not a comment"}`)
	got := string(StripJSONC(input))
	if !contains(got, "// not a comment") {
		t.Errorf("string content was stripped: %s", got)
	}
}

func TestStripJSONC_EscapedQuotes(t *testing.T) {
	input := []byte(`{"val": "escaped \" quote // still string"}`)
	got := string(StripJSONC(input))
	if !contains(got, "// still string") {
		t.Errorf("string content was stripped after escaped quote: %s", got)
	}
}

func TestParseConfig_ImageBased(t *testing.T) {
	data := []byte(`{
  // Image-based config
  "name": "test container",
  "image": "mcr.microsoft.com/devcontainers/base:ubuntu",
  "features": {
    "ghcr.io/devcontainers/features/node:1": {"version": "22"}
  },
  "remoteEnv": {"FOO": "bar"},
  "remoteUser": "vscode"
}`)
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "test container" {
		t.Errorf("name = %q, want %q", cfg.Name, "test container")
	}
	if cfg.Image != "mcr.microsoft.com/devcontainers/base:ubuntu" {
		t.Errorf("image = %q", cfg.Image)
	}
	if len(cfg.Features) != 1 {
		t.Errorf("features len = %d, want 1", len(cfg.Features))
	}
	if cfg.RemoteUser != "vscode" {
		t.Errorf("remoteUser = %q", cfg.RemoteUser)
	}
	if cfg.RemoteEnv["FOO"] != "bar" {
		t.Errorf("remoteEnv[FOO] = %q", cfg.RemoteEnv["FOO"])
	}
}

func TestParseConfig_DockerfileBased(t *testing.T) {
	data := []byte(`{
  "build": {
    "dockerfile": "Dockerfile",
    "context": "..",
    "args": {"VARIANT": "3.9"}
  }
}`)
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Build == nil {
		t.Fatal("build is nil")
	}
	if cfg.Build.Dockerfile != "Dockerfile" {
		t.Errorf("dockerfile = %q", cfg.Build.Dockerfile)
	}
	if cfg.Build.Context != ".." {
		t.Errorf("context = %q", cfg.Build.Context)
	}
	if cfg.Build.Args["VARIANT"] != "3.9" {
		t.Errorf("args[VARIANT] = %q", cfg.Build.Args["VARIANT"])
	}
}

func TestParseConfig_LifecycleCommands(t *testing.T) {
	data := []byte(`{
  "image": "ubuntu",
  "postStartCommand": "echo hello",
  "onCreateCommand": ["npm", "install"],
  "postCreateCommand": {"setup": "make setup", "lint": "make lint"}
}`)
	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}

	// String command.
	if s, ok := cfg.PostStartCommand.(string); !ok || s != "echo hello" {
		t.Errorf("postStartCommand = %v", cfg.PostStartCommand)
	}

	// Array command.
	if arr, ok := cfg.OnCreateCommand.([]interface{}); !ok || len(arr) != 2 {
		t.Errorf("onCreateCommand = %v", cfg.OnCreateCommand)
	}

	// Map command (parallel).
	if m, ok := cfg.PostCreateCommand.(map[string]interface{}); !ok || len(m) != 2 {
		t.Errorf("postCreateCommand = %v", cfg.PostCreateCommand)
	}
}

func TestParseConfig_InvalidJSON(t *testing.T) {
	_, err := ParseConfig([]byte(`{invalid`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFindConfig_Standard(t *testing.T) {
	dir := t.TempDir()
	dcDir := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(dcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dcDir, "devcontainer.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := FindConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(path)) != ".devcontainer" {
		t.Errorf("unexpected path: %s", path)
	}
}

func TestFindConfig_RootLevel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".devcontainer.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := FindConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != ".devcontainer.json" {
		t.Errorf("unexpected path: %s", path)
	}
}

func TestFindConfig_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := FindConfig(dir)
	if err == nil {
		t.Error("expected error when config not found")
	}
}

func TestResolveVariables_LocalEnv(t *testing.T) {
	t.Setenv("TEST_VAR_RESOLVE", "resolved-value")

	cfg := &DevcontainerConfig{
		RemoteEnv: map[string]string{
			"MY_VAR": "${localEnv:TEST_VAR_RESOLVE}",
		},
	}
	resolved := ResolveVariables(cfg, "/tmp/project")
	if resolved.RemoteEnv["MY_VAR"] != "resolved-value" {
		t.Errorf("MY_VAR = %q, want %q", resolved.RemoteEnv["MY_VAR"], "resolved-value")
	}
}

func TestResolveVariables_LocalEnvDefault(t *testing.T) {
	// Ensure the variable does not exist.
	t.Setenv("NONEXISTENT_VAR_FOR_TEST", "")
	os.Unsetenv("NONEXISTENT_VAR_FOR_TEST") //nolint:errcheck // test cleanup

	cfg := &DevcontainerConfig{
		RemoteEnv: map[string]string{
			"MY_VAR": "${localEnv:NONEXISTENT_VAR_FOR_TEST:fallback}",
		},
	}
	resolved := ResolveVariables(cfg, "/tmp/project")
	if resolved.RemoteEnv["MY_VAR"] != "fallback" {
		t.Errorf("MY_VAR = %q, want %q", resolved.RemoteEnv["MY_VAR"], "fallback")
	}
}

func TestResolveVariables_WorkspaceFolder(t *testing.T) {
	cfg := &DevcontainerConfig{
		WorkspaceFolder: "/workspaces/${localWorkspaceFolderBasename}",
	}
	resolved := ResolveVariables(cfg, "/home/user/my-project")
	if resolved.WorkspaceFolder != "/workspaces/my-project" {
		t.Errorf("workspaceFolder = %q", resolved.WorkspaceFolder)
	}
}

func TestResolveVariables_MountStrings(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	cfg := &DevcontainerConfig{
		Mounts: []interface{}{
			"source=${localEnv:HOME}/.human/ca.crt,target=/tmp/ca.crt,type=bind",
			42, // non-string entries should be left alone
		},
	}
	resolved := ResolveVariables(cfg, "/tmp/project")
	s, ok := resolved.Mounts[0].(string)
	if !ok {
		t.Fatalf("mount[0] is not string: %T", resolved.Mounts[0])
	}
	if !contains(s, "/home/testuser/.human/ca.crt") {
		t.Errorf("mount[0] = %q, expected resolved HOME", s)
	}
	// Non-string preserved.
	if _, ok := resolved.Mounts[1].(int); !ok {
		t.Errorf("mount[1] should be int, got %T", resolved.Mounts[1])
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && containsStr(haystack, needle)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
