package devcontainer

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestFeatureEnv_BasicOptions(t *testing.T) {
	opts := map[string]interface{}{
		"version": "22",
	}
	meta := &FeatureMeta{
		Options: map[string]FeatureOption{
			"version": {Type: "string", Default: "lts"},
		},
	}
	env := featureEnv(opts, meta, "vscode")

	envMap := toEnvMap(env)
	if envMap["VERSION"] != "22" {
		t.Errorf("VERSION = %q, want %q", envMap["VERSION"], "22")
	}
	if envMap["_REMOTE_USER"] != "vscode" {
		t.Errorf("_REMOTE_USER = %q", envMap["_REMOTE_USER"])
	}
	if envMap["_REMOTE_USER_HOME"] != "/home/vscode" {
		t.Errorf("_REMOTE_USER_HOME = %q", envMap["_REMOTE_USER_HOME"])
	}
}

func TestFeatureEnv_Defaults(t *testing.T) {
	meta := &FeatureMeta{
		Options: map[string]FeatureOption{
			"version": {Type: "string", Default: "lts"},
			"install": {Type: "boolean", Default: true},
		},
	}
	env := featureEnv(nil, meta, "root")

	envMap := toEnvMap(env)
	if envMap["VERSION"] != "lts" {
		t.Errorf("VERSION = %q, want %q", envMap["VERSION"], "lts")
	}
	if envMap["INSTALL"] != "true" {
		t.Errorf("INSTALL = %q, want %q", envMap["INSTALL"], "true")
	}
	if envMap["_REMOTE_USER_HOME"] != "/root" {
		t.Errorf("_REMOTE_USER_HOME = %q", envMap["_REMOTE_USER_HOME"])
	}
}

func TestFeatureEnv_OverridesDefaults(t *testing.T) {
	opts := map[string]interface{}{"version": "20"}
	meta := &FeatureMeta{
		Options: map[string]FeatureOption{
			"version": {Type: "string", Default: "lts"},
		},
	}
	env := featureEnv(opts, meta, "vscode")
	envMap := toEnvMap(env)
	if envMap["VERSION"] != "20" {
		t.Errorf("VERSION = %q, want %q (user override should win)", envMap["VERSION"], "20")
	}
}

func TestExtractFeatureMeta(t *testing.T) {
	tarData := buildFeatureTar(t, "test", "1.0.0")
	parsedMeta, err := extractFeatureMeta(tarData, "test-ref")
	if err != nil {
		t.Fatal(err)
	}
	if parsedMeta.ID != "test" {
		t.Errorf("meta.ID = %q", parsedMeta.ID)
	}
}

func TestExtractFeatureMeta_NoMetaFile(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "./install.sh", Size: 5, Mode: 0o755})
	_, _ = tw.Write([]byte("#!/sh"))
	_ = tw.Close()

	meta, err := extractFeatureMeta(buf.Bytes(), "test-ref")
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != "" {
		t.Errorf("expected empty meta ID, got %q", meta.ID)
	}
}

// buildFeatureTar creates a minimal feature tarball for testing.
func buildFeatureTar(t *testing.T, id, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	script := []byte("#!/bin/sh\necho hello\n")
	_ = tw.WriteHeader(&tar.Header{Name: "./install.sh", Size: int64(len(script)), Mode: 0o755})
	_, _ = tw.Write(script)

	meta := FeatureMeta{ID: id, Version: version, Name: "Test Feature"}
	metaJSON, _ := json.Marshal(meta)
	_ = tw.WriteHeader(&tar.Header{Name: "./devcontainer-feature.json", Size: int64(len(metaJSON)), Mode: 0o644})
	_, _ = tw.Write(metaJSON)
	_ = tw.Close()
	return buf.Bytes()
}

func TestInstallFeatures_ExecCalls(t *testing.T) {
	mock := &mockDockerClient{}

	meta := &FeatureMeta{
		ID:      "node",
		Options: map[string]FeatureOption{"version": {Default: "lts"}},
	}
	tarData := buildFeatureTar(t, "node", "1.0.0")
	puller := &mockFeaturePuller{
		tarData: tarData,
		meta:    meta,
	}

	features := map[string]interface{}{
		"ghcr.io/devcontainers/features/node:1": map[string]interface{}{"version": "22"},
	}

	err := InstallFeatures(context.Background(), mock, puller, "container-123",
		features, "vscode", testLogger(), &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}

	// Should have 3 exec calls: mkdir, run install.sh, cleanup.
	if len(mock.execCalls) < 2 {
		t.Fatalf("expected at least 2 exec calls, got %d", len(mock.execCalls))
	}

	// The run call (second) should have env vars.
	runCall := mock.execCalls[1]
	if runCall.Opts.User != "root" {
		t.Errorf("run call user = %q, want root", runCall.Opts.User)
	}
	envMap := toEnvMap(runCall.Opts.Env)
	if envMap["VERSION"] != "22" {
		t.Errorf("VERSION env = %q, want 22", envMap["VERSION"])
	}
	if envMap["_REMOTE_USER"] != "vscode" {
		t.Errorf("_REMOTE_USER = %q", envMap["_REMOTE_USER"])
	}
}

func TestInstallFeatures_Empty(t *testing.T) {
	err := InstallFeatures(context.Background(), &mockDockerClient{}, &mockFeaturePuller{},
		"cid", nil, "user", testLogger(), &strings.Builder{})
	if err != nil {
		t.Errorf("expected nil error for empty features: %v", err)
	}
}

func TestSortedFeatureRefs(t *testing.T) {
	features := map[string]interface{}{
		"ghcr.io/z/feature:1": nil,
		"ghcr.io/a/feature:1": nil,
		"ghcr.io/m/feature:1": nil,
	}
	refs := sortedFeatureRefs(features)
	if refs[0] != "ghcr.io/a/feature:1" || refs[2] != "ghcr.io/z/feature:1" {
		t.Errorf("not sorted: %v", refs)
	}
}

// mockFeaturePuller returns pre-configured feature content.
type mockFeaturePuller struct {
	tarData []byte
	meta    *FeatureMeta
	err     error
}

func (m *mockFeaturePuller) Pull(_ context.Context, _ string) ([]byte, *FeatureMeta, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	return m.tarData, m.meta, nil
}

func toEnvMap(env []string) map[string]string {
	m := make(map[string]string)
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}
