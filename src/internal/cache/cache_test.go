package cache

import (
	"path/filepath"
	"reflect"
	"testing"

	"terradrift/src/internal/model"
)

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "last.json")
	report := model.ScanReport{
		SchemaVersion: 1,
		Metadata: model.Metadata{
			Provider:      "gcp",
			Project:       "demo-project",
			Path:          ".",
			ResourceTypes: []string{model.ResourceTypeBucket},
			Version:       "test",
		},
		Findings: []model.Finding{{
			ID:           "finding-1",
			ResourceType: model.ResourceTypeBucket,
			FindingType:  model.FindingTypeExtra,
			Severity:     model.SeverityMedium,
			Cause:        model.CauseLegacyArtifact,
		}},
	}

	if err := Save(report, path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !reflect.DeepEqual(got, report) {
		t.Fatalf("loaded report mismatch:\ngot=%+v\nwant=%+v", got, report)
	}
}
