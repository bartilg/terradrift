package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"terradrift/src/internal/model"
)

func TestResolveConfigPathPrefersPathHintConfig(t *testing.T) {
	tempDir := t.TempDir()
	projectDir := filepath.Join(tempDir, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	globalCfg := filepath.Join(tempDir, "terradrift.yaml")
	projectCfg := filepath.Join(projectDir, "terradrift.yaml")
	if err := os.WriteFile(globalCfg, []byte("project: \"global\"\n"), 0o644); err != nil {
		t.Fatalf("write global config failed: %v", err)
	}
	if err := os.WriteFile(projectCfg, []byte("project: \"project\"\n"), 0o644); err != nil {
		t.Fatalf("write project config failed: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}

	resolved, err := ResolveConfigPath("", projectDir)
	if err != nil {
		t.Fatalf("ResolveConfigPath failed: %v", err)
	}
	if resolved != projectCfg {
		t.Fatalf("resolved config mismatch: got=%s want=%s", resolved, projectCfg)
	}
}

func TestLoadFileExpandsFromDotEnvAndEnvironment(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "terradrift.yaml")
	envPath := filepath.Join(tempDir, ".env")

	if err := os.WriteFile(envPath, []byte("GCP_PROJECT_ID=env-file-project\nTD_FORMAT=text\n"), 0o644); err != nil {
		t.Fatalf("write .env failed: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte("env_file: .env\nproject: ${GCP_PROJECT_ID}\nformat: ${TD_FORMAT}\n"), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	t.Setenv("TD_FORMAT", "json")
	cfg, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if cfg.Project != "env-file-project" {
		t.Fatalf("project expansion mismatch: got=%s", cfg.Project)
	}
	if cfg.Format != "json" {
		t.Fatalf("environment override mismatch: got=%s", cfg.Format)
	}
}

func TestLoadFileAcceptsHyphenatedIgnoreDefaults(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "terradrift.yaml")
	if err := os.WriteFile(cfgPath, []byte("ignore-defaults: true\n"), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if !cfg.IgnoreDefaults {
		t.Fatalf("expected hyphenated ignore-defaults to enable IgnoreDefaults")
	}
}

func TestMergeResolvesConfigRelativePath(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "terradrift.yaml")
	defaults := DefaultScanOptions()
	merged, err := Merge(defaults, FileConfig{Path: ".", Project: "demo"}, ScanOptions{ConfigPath: cfgPath})
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}
	if merged.Path != tempDir {
		t.Fatalf("merged path mismatch: got=%s want=%s", merged.Path, tempDir)
	}
}

func TestDefaultScanOptionsIncludesExpandedGCPResources(t *testing.T) {
	defaults := DefaultScanOptions()
	if got, want := defaults.ResourceTypes, model.SupportedResourceTypes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("default resource types mismatch:\ngot=%v\nwant=%v", got, want)
	}
}

func TestMergeEnablesIgnoreDefaultsFromConfigAndCLI(t *testing.T) {
	defaults := DefaultScanOptions()

	fromConfig, err := Merge(defaults, FileConfig{IgnoreDefaults: true}, ScanOptions{})
	if err != nil {
		t.Fatalf("Merge with config ignore_defaults failed: %v", err)
	}
	if !fromConfig.IgnoreDefaults {
		t.Fatalf("expected ignore defaults to be enabled from config")
	}

	fromCLI, err := Merge(defaults, FileConfig{}, ScanOptions{IgnoreDefaults: true})
	if err != nil {
		t.Fatalf("Merge with CLI ignore-defaults failed: %v", err)
	}
	if !fromCLI.IgnoreDefaults {
		t.Fatalf("expected ignore defaults to be enabled from CLI")
	}
}

func TestParseResourceTypesArg(t *testing.T) {
	got := ParseResourceTypesArg(" google_storage_bucket, ,google_service_account ")
	want := []string{model.ResourceTypeBucket, model.ResourceTypeServiceAccount}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseResourceTypesArg mismatch: got=%v want=%v", got, want)
	}

	if got := ParseResourceTypesArg(" "); got != nil {
		t.Fatalf("blank resource types should parse to nil, got=%v", got)
	}
}

func TestConfigTemplateContainsEverySupportedResourceType(t *testing.T) {
	template := ConfigTemplate()
	for _, resourceType := range model.SupportedResourceTypes() {
		if !strings.Contains(template, "- "+resourceType) {
			t.Fatalf("config template missing supported resource type %s", resourceType)
		}
	}
}

func TestParseDotEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(`
# comment
export GCP_PROJECT_ID="demo-project"
TD_FORMAT='json'
TD_OUTPUT=out.json # inline comment
`), 0o644); err != nil {
		t.Fatalf("write .env failed: %v", err)
	}

	values, err := parseDotEnvFile(path)
	if err != nil {
		t.Fatalf("parseDotEnvFile failed: %v", err)
	}
	want := map[string]string{
		"GCP_PROJECT_ID": "demo-project",
		"TD_FORMAT":      "json",
		"TD_OUTPUT":      "out.json",
	}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf(".env values mismatch: got=%v want=%v", values, want)
	}
}

func TestParseDotEnvFileRejectsInvalidLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("invalid\n"), 0o644); err != nil {
		t.Fatalf("write .env failed: %v", err)
	}

	if _, err := parseDotEnvFile(path); err == nil {
		t.Fatalf("expected parse error for invalid .env line")
	}
}
