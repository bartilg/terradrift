package report

import (
	"encoding/json"
	"strings"
	"testing"

	"terradrift/src/internal/model"
)

func TestToJSON(t *testing.T) {
	report := sampleReport()
	got, err := ToJSON(report)
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	if !strings.HasSuffix(string(got), "\n") {
		t.Fatalf("ToJSON should append trailing newline")
	}

	var decoded model.ScanReport
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("ToJSON emitted invalid JSON: %v", err)
	}
	if decoded.SchemaVersion != report.SchemaVersion {
		t.Fatalf("decoded report mismatch: got schema=%d want=%d", decoded.SchemaVersion, report.SchemaVersion)
	}
}

func TestToText(t *testing.T) {
	got := ToText(sampleReport())
	for _, want := range []string{"Summary", "Expected: 1", "Observed: 1", "Findings: 1", "MISMATCH", "labels"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ToText output missing %q:\n%s", want, got)
		}
	}

	empty := ToText(model.ScanReport{})
	if !strings.Contains(empty, "Findings\n  none") {
		t.Fatalf("empty report should show no findings:\n%s", empty)
	}
}

func TestExplainText(t *testing.T) {
	got, err := ExplainText(sampleReport(), "finding-1")
	if err != nil {
		t.Fatalf("ExplainText failed: %v", err)
	}
	for _, want := range []string{"Finding finding-1", "Intent", "Observed", "Diff", "classification_rule: label_only"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ExplainText output missing %q:\n%s", want, got)
		}
	}

	if _, err := ExplainText(sampleReport(), "missing"); err == nil {
		t.Fatalf("expected missing finding error")
	}
}

func sampleReport() model.ScanReport {
	return model.ScanReport{
		SchemaVersion: 1,
		Metadata: model.Metadata{
			Provider:      "gcp",
			Project:       "demo-project",
			Path:          ".",
			ResourceTypes: []string{model.ResourceTypeBucket},
			Version:       "test",
		},
		ExpectedResources: []model.ExpectedResource{{
			Address:      "google_storage_bucket.logs",
			ResourceType: model.ResourceTypeBucket,
			Name:         "logs",
			Identity:     map[string]any{"name": "logs"},
			Normalized:   map[string]any{"labels": map[string]string{"env": "dev"}},
		}},
		ObservedResources: []model.ObservedResource{{
			ResourceType: model.ResourceTypeBucket,
			ProviderID:   "projects/_/buckets/logs",
			Identity:     map[string]any{"name": "logs"},
			Normalized:   map[string]any{"labels": map[string]string{"env": "prod"}},
		}},
		Findings: []model.Finding{{
			ID:               "finding-1",
			ResourceType:     model.ResourceTypeBucket,
			FindingType:      model.FindingTypeMismatch,
			Severity:         model.SeverityMedium,
			Cause:            model.CauseManualChange,
			ExpectedAddress:  "google_storage_bucket.logs",
			ObservedProvider: "projects/_/buckets/logs",
			Diff: []model.DiffEntry{{
				Field:    "labels",
				Expected: map[string]string{"env": "dev"},
				Observed: map[string]string{"env": "prod"},
			}},
			Evidence: map[string]any{"classification_rule": "label_only"},
		}},
	}
}
