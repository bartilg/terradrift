package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"terradrift/src/internal/config"
	"terradrift/src/internal/model"
	"terradrift/src/internal/report"
)

type goldenObserver struct{}

func (goldenObserver) Observe(ctx context.Context, project string, resourceTypes []string) ([]model.ObservedResource, error) {
	return []model.ObservedResource{
		{
			ResourceType: model.ResourceTypeBucket,
			ProviderID:   "projects/_/buckets/td-logs-bucket",
			Identity:     map[string]any{"name": "td-logs-bucket"},
			Normalized: map[string]any{
				"name":                        "td-logs-bucket",
				"location":                    "US",
				"storage_class":               "STANDARD",
				"uniform_bucket_level_access": false,
				"versioning_enabled":          true,
				"labels": map[string]string{
					"env": "prod",
				},
			},
		},
		{
			ResourceType: model.ResourceTypeBucket,
			ProviderID:   "projects/_/buckets/legacy-bucket",
			Identity:     map[string]any{"name": "legacy-bucket"},
			Normalized: map[string]any{
				"name":                        "legacy-bucket",
				"location":                    "US",
				"storage_class":               "STANDARD",
				"uniform_bucket_level_access": false,
				"versioning_enabled":          false,
				"labels":                      map[string]string{},
			},
		},
		{
			ResourceType: model.ResourceTypeServiceAccount,
			ProviderID:   "projects/demo-project/serviceAccounts/old-sa@demo-project.iam.gserviceaccount.com",
			Identity: map[string]any{
				"email": "old-sa@demo-project.iam.gserviceaccount.com",
			},
			Normalized: map[string]any{
				"account_id":   "old-sa",
				"project":      "demo-project",
				"display_name": "Old Service Account",
				"description":  "legacy",
				"disabled":     false,
			},
		},
	}, nil
}

func TestGoldenScanReportJSON(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "..", "testdata", "fixtures", "basic")
	svc := NewService(goldenObserver{}, "test-version")

	reportData, err := svc.Run(context.Background(), config.ScanOptions{
		Path:          fixturePath,
		Project:       "demo-project",
		ResourceTypes: []string{model.ResourceTypeBucket, model.ResourceTypeServiceAccount},
	})
	if err != nil {
		t.Fatalf("scan run failed: %v", err)
	}

	got, err := report.ToJSON(reportData)
	if err != nil {
		t.Fatalf("marshal json failed: %v", err)
	}

	goldenPath := filepath.Join("..", "..", "..", "testdata", "golden", "basic_scan.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden file failed: %v", err)
		}
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file failed: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("golden mismatch: got differs from %s\nset UPDATE_GOLDEN=1 to update", goldenPath)
	}
}
