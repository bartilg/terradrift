package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"terradrift/src/internal/config"
	"terradrift/src/internal/model"
)

type recordingObserver struct {
	project       string
	resourceTypes []string
	resources     []model.ObservedResource
}

func (o *recordingObserver) Observe(ctx context.Context, project string, resourceTypes []string) ([]model.ObservedResource, error) {
	o.project = project
	o.resourceTypes = append([]string{}, resourceTypes...)
	out := make([]model.ObservedResource, len(o.resources))
	copy(out, o.resources)
	return out, nil
}

func TestScanCommandAcceptsConfigShorthand(t *testing.T) {
	tempDir := t.TempDir()
	tfDir := filepath.Join(tempDir, "terraform")
	if err := os.MkdirAll(tfDir, 0o755); err != nil {
		t.Fatalf("mkdir terraform dir failed: %v", err)
	}
	cfgPath := filepath.Join(tempDir, "custom.yaml")
	cfg := []byte(`
project: "demo-project"
path: "terraform"
format: "json"
fail_on: "never"
resource_types:
  - google_storage_bucket
`)
	if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}

	observer := &recordingObserver{resources: []model.ObservedResource{{
		ResourceType: model.ResourceTypeBucket,
		ProviderID:   "projects/_/buckets/example",
		Identity:     map[string]any{"name": "example"},
		Normalized:   map[string]any{"name": "example"},
	}}}
	var stdout, stderr bytes.Buffer
	cmd := NewRootCmd(Deps{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Version:  "test",
		Observer: observer,
	})
	cmd.SetArgs([]string{"scan", "-f", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("scan -f failed: %v\nstderr=%s", err, stderr.String())
	}
	if got, want := observer.project, "demo-project"; got != want {
		t.Fatalf("observer project mismatch: got=%s want=%s", got, want)
	}
	if got, want := observer.resourceTypes, []string{model.ResourceTypeBucket}; !reflect.DeepEqual(got, want) {
		t.Fatalf("observer resource types mismatch: got=%v want=%v", got, want)
	}
	if !strings.Contains(stdout.String(), `"project": "demo-project"`) {
		t.Fatalf("scan output did not use config from -f: %s", stdout.String())
	}
}

func TestValidateEmptyResultReturnsErrorForEmptyIntentAndObserved(t *testing.T) {
	err := validateEmptyResult(
		config.ScanOptions{Project: "demo-project", ResourceTypes: []string{model.ResourceTypeBucket, model.ResourceTypeServiceAccount}},
		model.ScanReport{},
	)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "do not exist") {
		t.Fatalf("error should mention missing resources, got: %s", msg)
	}
	if !strings.Contains(msg, "project mapping is incorrect") {
		t.Fatalf("error should mention incorrect mapping, got: %s", msg)
	}
}

func TestValidateEmptyResultAllowsNonEmptyObserved(t *testing.T) {
	err := validateEmptyResult(
		config.ScanOptions{Project: "demo-project", ResourceTypes: []string{model.ResourceTypeBucket}},
		model.ScanReport{
			ObservedResources: []model.ObservedResource{{
				ResourceType: model.ResourceTypeBucket,
				ProviderID:   "projects/_/buckets/example",
				Identity:     map[string]any{"name": "example"},
				Normalized:   map[string]any{"name": "example"},
			}},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEmptyResultAllowsNonEmptyExpected(t *testing.T) {
	err := validateEmptyResult(
		config.ScanOptions{Project: "demo-project", ResourceTypes: []string{model.ResourceTypeBucket}},
		model.ScanReport{
			ExpectedResources: []model.ExpectedResource{{
				Address:      "google_storage_bucket.example",
				ResourceType: model.ResourceTypeBucket,
				Name:         "example",
				Identity:     map[string]any{"name": "example"},
				Normalized:   map[string]any{"name": "example"},
			}},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateResourceTypesAllowsExpandedGCPResources(t *testing.T) {
	if err := validateResourceTypes(model.SupportedResourceTypes()); err != nil {
		t.Fatalf("validateResourceTypes returned unexpected error: %v", err)
	}
}
