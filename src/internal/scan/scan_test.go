package scan

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"terradrift/src/internal/config"
	"terradrift/src/internal/model"
)

type mockObserver struct {
	resources []model.ObservedResource
}

func (m mockObserver) Observe(ctx context.Context, project string, resourceTypes []string) ([]model.ObservedResource, error) {
	out := make([]model.ObservedResource, len(m.resources))
	copy(out, m.resources)
	return out, nil
}

func TestDiffIgnoresUnknownIntentFields(t *testing.T) {
	expected := model.ExpectedResource{
		Address:      "google_storage_bucket.logs",
		ResourceType: model.ResourceTypeBucket,
		Identity:     map[string]any{"name": "td-logs-bucket"},
		Normalized: map[string]any{
			"name":          "td-logs-bucket",
			"storage_class": "STANDARD",
		},
		FieldExplicit: map[string]bool{
			"name":          true,
			"storage_class": true,
		},
		IdentityKnown: true,
	}
	observed := model.ObservedResource{
		ResourceType: model.ResourceTypeBucket,
		ProviderID:   "projects/_/buckets/td-logs-bucket",
		Identity:     map[string]any{"name": "td-logs-bucket"},
		Normalized: map[string]any{
			"name":          "td-logs-bucket",
			"location":      "EU",
			"storage_class": "NEARLINE",
		},
	}

	diffs := diffExpectedObserved(expected, observed)
	if got, want := len(diffs), 1; got != want {
		t.Fatalf("diff count mismatch: got=%d want=%d diffs=%+v", got, want, diffs)
	}
	if got, want := diffs[0].Field, "storage_class"; got != want {
		t.Fatalf("unexpected diff field: got=%s want=%s", got, want)
	}
}

func TestDiffTreatsLabelsAsManagedKeySubset(t *testing.T) {
	expected := model.ExpectedResource{
		Address:      "google_storage_bucket.logs",
		ResourceType: model.ResourceTypeBucket,
		Identity:     map[string]any{"name": "td-logs-bucket"},
		Normalized: map[string]any{
			"labels": map[string]string{
				"env": "dev",
			},
		},
		FieldExplicit: map[string]bool{"labels": true},
		IdentityKnown: true,
	}
	observed := model.ObservedResource{
		ResourceType: model.ResourceTypeBucket,
		ProviderID:   "projects/_/buckets/td-logs-bucket",
		Identity:     map[string]any{"name": "td-logs-bucket"},
		Normalized: map[string]any{
			"labels": map[string]string{
				"env":                        "dev",
				"goog-terraform-provisioned": "true",
			},
		},
	}

	diffs := diffExpectedObserved(expected, observed)
	if got, want := len(diffs), 0; got != want {
		t.Fatalf("diff count mismatch: got=%d want=%d diffs=%+v", got, want, diffs)
	}
}

func TestDiffReportsManagedLabelValueMismatch(t *testing.T) {
	expected := model.ExpectedResource{
		Address:      "google_storage_bucket.logs",
		ResourceType: model.ResourceTypeBucket,
		Identity:     map[string]any{"name": "td-logs-bucket"},
		Normalized: map[string]any{
			"labels": map[string]string{
				"env": "dev",
			},
		},
		FieldExplicit: map[string]bool{"labels": true},
		IdentityKnown: true,
	}
	observed := model.ObservedResource{
		ResourceType: model.ResourceTypeBucket,
		ProviderID:   "projects/_/buckets/td-logs-bucket",
		Identity:     map[string]any{"name": "td-logs-bucket"},
		Normalized: map[string]any{
			"labels": map[string]string{
				"env":                        "prod",
				"goog-terraform-provisioned": "true",
			},
		},
	}

	diffs := diffExpectedObserved(expected, observed)
	if got, want := len(diffs), 1; got != want {
		t.Fatalf("diff count mismatch: got=%d want=%d diffs=%+v", got, want, diffs)
	}
	if got, want := diffs[0].Field, "labels"; got != want {
		t.Fatalf("unexpected diff field: got=%s want=%s", got, want)
	}
}

func TestRunDeterministicOutput(t *testing.T) {
	parser := func(path string, project string, resourceTypes []string) ([]model.ExpectedResource, error) {
		return []model.ExpectedResource{
			{
				Address:       "google_storage_bucket.logs",
				ResourceType:  model.ResourceTypeBucket,
				Name:          "logs",
				Identity:      map[string]any{"name": "td-logs-bucket"},
				IdentityKnown: true,
				Normalized: map[string]any{
					"name":                        "td-logs-bucket",
					"location":                    "US",
					"storage_class":               "STANDARD",
					"uniform_bucket_level_access": false,
					"versioning_enabled":          false,
					"labels": map[string]string{
						"team": "platform",
						"env":  "dev",
					},
				},
				FieldExplicit: map[string]bool{"name": true, "location": true, "storage_class": true, "labels": true},
			},
		}, nil
	}

	observer := mockObserver{resources: []model.ObservedResource{
		{
			ResourceType: model.ResourceTypeBucket,
			ProviderID:   "projects/_/buckets/td-logs-bucket",
			Identity:     map[string]any{"name": "td-logs-bucket"},
			Normalized: map[string]any{
				"name":                        "td-logs-bucket",
				"location":                    "US",
				"storage_class":               "STANDARD",
				"uniform_bucket_level_access": false,
				"versioning_enabled":          false,
				"labels": map[string]string{
					"env":  "dev",
					"team": "platform",
				},
			},
		},
	}}

	svc := NewService(observer, "test")
	svc.ParseIntent = parser
	opts := config.ScanOptions{
		Path:          ".",
		Project:       "demo-project",
		ResourceTypes: []string{model.ResourceTypeBucket},
	}

	first, err := svc.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	second, err := svc.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}

	b1, _ := json.Marshal(first)
	b2, _ := json.Marshal(second)
	if !reflect.DeepEqual(b1, b2) {
		t.Fatalf("scan output changed across identical runs")
	}
}

func TestRunIgnoreDefaultsSkipsKnownDefaultObservedResources(t *testing.T) {
	parser := func(path string, project string, resourceTypes []string) ([]model.ExpectedResource, error) {
		return nil, nil
	}

	observer := mockObserver{resources: []model.ObservedResource{
		{
			ResourceType: model.ResourceTypeComputeNetwork,
			ProviderID:   "https://www.googleapis.com/compute/v1/projects/demo/global/networks/default",
			Identity:     map[string]any{"name": "default"},
			Normalized: map[string]any{
				"name":                    "default",
				"auto_create_subnetworks": true,
				"routing_mode":            "REGIONAL",
			},
		},
		{
			ResourceType: model.ResourceTypeComputeSubnetwork,
			ProviderID:   "projects/demo/regions/us-central1/subnetworks/default",
			Identity:     map[string]any{"name": "default", "region": "us-central1"},
			Normalized: map[string]any{
				"name":                     "default",
				"network":                  "default",
				"region":                   "us-central1",
				"ip_cidr_range":            "10.128.0.0/20",
				"private_ip_google_access": false,
			},
		},
		{
			ResourceType: model.ResourceTypeServiceAccount,
			ProviderID:   "projects/demo/serviceAccounts/123456789-compute@developer.gserviceaccount.com",
			Identity:     map[string]any{"email": "123456789-compute@developer.gserviceaccount.com"},
			Normalized: map[string]any{
				"account_id":   "123456789-compute",
				"project":      "developer",
				"display_name": "Compute Engine default service account",
				"description":  "",
				"disabled":     false,
			},
		},
		{
			ResourceType: model.ResourceTypeCloudRunService,
			ProviderID:   "projects/demo/locations/us-central1/services/td-unmanaged-run-1770401311",
			Identity:     map[string]any{"name": "td-unmanaged-run-1770401311", "location": "us-central1"},
			Normalized: map[string]any{
				"name":     "td-unmanaged-run-1770401311",
				"location": "us-central1",
			},
		},
		{
			ResourceType: model.ResourceTypeComputeNetwork,
			ProviderID:   "projects/demo/global/networks/custom",
			Identity:     map[string]any{"name": "custom"},
			Normalized: map[string]any{
				"name":                    "custom",
				"auto_create_subnetworks": false,
				"routing_mode":            "GLOBAL",
			},
		},
	}}

	svc := NewService(observer, "test")
	svc.ParseIntent = parser

	reportWithoutFilter, err := svc.Run(context.Background(), config.ScanOptions{
		Path:          ".",
		Project:       "demo-project",
		ResourceTypes: []string{model.ResourceTypeCloudRunService, model.ResourceTypeComputeSubnetwork, model.ResourceTypeComputeNetwork, model.ResourceTypeServiceAccount},
	})
	if err != nil {
		t.Fatalf("run without ignore-defaults failed: %v", err)
	}
	if got, want := len(reportWithoutFilter.ObservedResources), 5; got != want {
		t.Fatalf("observed resources without filter mismatch: got=%d want=%d", got, want)
	}
	if got, want := len(reportWithoutFilter.Findings), 5; got != want {
		t.Fatalf("findings without filter mismatch: got=%d want=%d", got, want)
	}

	reportWithFilter, err := svc.Run(context.Background(), config.ScanOptions{
		Path:           ".",
		Project:        "demo-project",
		ResourceTypes:  []string{model.ResourceTypeCloudRunService, model.ResourceTypeComputeSubnetwork, model.ResourceTypeComputeNetwork, model.ResourceTypeServiceAccount},
		IgnoreDefaults: true,
	})
	if err != nil {
		t.Fatalf("run with ignore-defaults failed: %v", err)
	}
	if !reportWithFilter.Metadata.IgnoreDefaults {
		t.Fatalf("expected metadata to record ignore-defaults")
	}
	if got, want := len(reportWithFilter.ObservedResources), 2; got != want {
		t.Fatalf("observed resources with filter mismatch: got=%d want=%d", got, want)
	}
	if got, want := len(reportWithFilter.Findings), 2; got != want {
		t.Fatalf("findings with filter mismatch: got=%d want=%d", got, want)
	}
	remaining := map[string]bool{}
	for _, resource := range reportWithFilter.ObservedResources {
		remaining[resource.ProviderID] = true
	}
	for _, want := range []string{
		"projects/demo/global/networks/custom",
		"projects/demo/locations/us-central1/services/td-unmanaged-run-1770401311",
	} {
		if !remaining[want] {
			t.Fatalf("expected remaining observed resource %s, got %+v", want, reportWithFilter.ObservedResources)
		}
	}
}

func TestDefaultServiceAccountEmailRecognition(t *testing.T) {
	tests := []struct {
		email string
		want  bool
	}{
		{email: "123456789-compute@developer.gserviceaccount.com", want: true},
		{email: "demo-project@appspot.gserviceaccount.com", want: true},
		{email: "123456789@cloudbuild.gserviceaccount.com", want: true},
		{email: "app-sa@demo-project.iam.gserviceaccount.com", want: false},
	}

	for _, tt := range tests {
		if got := isDefaultServiceAccountEmail(tt.email); got != tt.want {
			t.Fatalf("isDefaultServiceAccountEmail(%q)=%v want=%v", tt.email, got, tt.want)
		}
	}
}

func TestRunFiltersObservedResourcesOutsideConfiguredResourceTypes(t *testing.T) {
	parser := func(path string, project string, resourceTypes []string) ([]model.ExpectedResource, error) {
		return nil, nil
	}

	observer := mockObserver{resources: []model.ObservedResource{
		{
			ResourceType: model.ResourceTypeBucket,
			ProviderID:   "projects/_/buckets/legacy-bucket",
			Identity:     map[string]any{"name": "legacy-bucket"},
			Normalized:   map[string]any{"name": "legacy-bucket"},
		},
		{
			ResourceType: model.ResourceTypeServiceAccount,
			ProviderID:   "projects/demo-project/serviceAccounts/old-sa@demo-project.iam.gserviceaccount.com",
			Identity:     map[string]any{"email": "old-sa@demo-project.iam.gserviceaccount.com"},
			Normalized:   map[string]any{"account_id": "old-sa"},
		},
	}}

	svc := NewService(observer, "test")
	svc.ParseIntent = parser

	reportData, err := svc.Run(context.Background(), config.ScanOptions{
		Path:          ".",
		Project:       "demo-project",
		ResourceTypes: []string{model.ResourceTypeBucket},
	})
	if err != nil {
		t.Fatalf("scan run failed: %v", err)
	}

	if got, want := len(reportData.ObservedResources), 1; got != want {
		t.Fatalf("observed resource count mismatch: got=%d want=%d resources=%+v", got, want, reportData.ObservedResources)
	}
	if got, want := reportData.ObservedResources[0].ResourceType, model.ResourceTypeBucket; got != want {
		t.Fatalf("observed resource type mismatch: got=%s want=%s", got, want)
	}
	if got, want := len(reportData.Findings), 1; got != want {
		t.Fatalf("finding count mismatch: got=%d want=%d findings=%+v", got, want, reportData.Findings)
	}
	if got, want := reportData.Findings[0].ResourceType, model.ResourceTypeBucket; got != want {
		t.Fatalf("finding resource type mismatch: got=%s want=%s", got, want)
	}
}

func TestRunComparesServiceAccountsByEmailIdentity(t *testing.T) {
	parser := func(path string, project string, resourceTypes []string) ([]model.ExpectedResource, error) {
		return []model.ExpectedResource{
			{
				Address:       "google_service_account.app",
				ResourceType:  model.ResourceTypeServiceAccount,
				Name:          "app",
				Identity:      map[string]any{"email": "app-sa@demo-project.iam.gserviceaccount.com"},
				IdentityKnown: true,
				Normalized: map[string]any{
					"account_id":   "app-sa",
					"project":      "demo-project",
					"display_name": "App Service Account",
					"description":  "Managed by Terraform",
					"disabled":     false,
				},
				FieldExplicit: map[string]bool{
					"account_id":   true,
					"project":      true,
					"display_name": true,
					"description":  true,
					"disabled":     true,
				},
			},
		}, nil
	}

	observer := mockObserver{resources: []model.ObservedResource{
		{
			ResourceType: model.ResourceTypeServiceAccount,
			ProviderID:   "projects/demo-project/serviceAccounts/app-sa@demo-project.iam.gserviceaccount.com",
			Identity:     map[string]any{"email": "app-sa@demo-project.iam.gserviceaccount.com"},
			Normalized: map[string]any{
				"account_id":   "app-sa",
				"project":      "demo-project",
				"display_name": "Changed out of band",
				"description":  "Managed by Terraform",
				"disabled":     false,
			},
		},
	}}

	svc := NewService(observer, "test")
	svc.ParseIntent = parser

	reportData, err := svc.Run(context.Background(), config.ScanOptions{
		Path:          ".",
		Project:       "demo-project",
		ResourceTypes: []string{model.ResourceTypeServiceAccount},
	})
	if err != nil {
		t.Fatalf("scan run failed: %v", err)
	}

	if got, want := len(reportData.Matches), 1; got != want {
		t.Fatalf("match count mismatch: got=%d want=%d", got, want)
	}
	if got, want := reportData.Matches[0].ExpectedAddress, "google_service_account.app"; got != want {
		t.Fatalf("matched address mismatch: got=%s want=%s", got, want)
	}
	if got, want := reportData.Matches[0].ObservedProvider, "projects/demo-project/serviceAccounts/app-sa@demo-project.iam.gserviceaccount.com"; got != want {
		t.Fatalf("matched provider mismatch: got=%s want=%s", got, want)
	}

	if got, want := len(reportData.Findings), 1; got != want {
		t.Fatalf("finding count mismatch: got=%d want=%d findings=%+v", got, want, reportData.Findings)
	}
	finding := reportData.Findings[0]
	if finding.FindingType != model.FindingTypeMismatch {
		t.Fatalf("finding type mismatch: got=%s want=%s", finding.FindingType, model.FindingTypeMismatch)
	}
	if finding.ResourceType != model.ResourceTypeServiceAccount {
		t.Fatalf("resource type mismatch: got=%s want=%s", finding.ResourceType, model.ResourceTypeServiceAccount)
	}
	if got, want := len(finding.Diff), 1; got != want {
		t.Fatalf("diff count mismatch: got=%d want=%d diff=%+v", got, want, finding.Diff)
	}
	if got, want := finding.Diff[0].Field, "display_name"; got != want {
		t.Fatalf("diff field mismatch: got=%s want=%s", got, want)
	}
}

func TestRunDoesNotMatchServiceAccountsAcrossProjects(t *testing.T) {
	parser := func(path string, project string, resourceTypes []string) ([]model.ExpectedResource, error) {
		return []model.ExpectedResource{
			{
				Address:       "google_service_account.app",
				ResourceType:  model.ResourceTypeServiceAccount,
				Name:          "app",
				Identity:      map[string]any{"email": "app-sa@other-project.iam.gserviceaccount.com"},
				IdentityKnown: true,
				Normalized: map[string]any{
					"account_id":   "app-sa",
					"project":      "other-project",
					"display_name": "App Service Account",
					"description":  "",
					"disabled":     false,
				},
				FieldExplicit: map[string]bool{"account_id": true, "project": true},
			},
		}, nil
	}

	observer := mockObserver{resources: []model.ObservedResource{
		{
			ResourceType: model.ResourceTypeServiceAccount,
			ProviderID:   "projects/demo-project/serviceAccounts/app-sa@demo-project.iam.gserviceaccount.com",
			Identity:     map[string]any{"email": "app-sa@demo-project.iam.gserviceaccount.com"},
			Normalized: map[string]any{
				"account_id":   "app-sa",
				"project":      "demo-project",
				"display_name": "App Service Account",
				"description":  "",
				"disabled":     false,
			},
		},
	}}

	svc := NewService(observer, "test")
	svc.ParseIntent = parser

	reportData, err := svc.Run(context.Background(), config.ScanOptions{
		Path:          ".",
		Project:       "demo-project",
		ResourceTypes: []string{model.ResourceTypeServiceAccount},
	})
	if err != nil {
		t.Fatalf("scan run failed: %v", err)
	}

	if got, want := len(reportData.Matches), 0; got != want {
		t.Fatalf("match count mismatch: got=%d want=%d", got, want)
	}
	if got, want := len(reportData.Findings), 2; got != want {
		t.Fatalf("finding count mismatch: got=%d want=%d findings=%+v", got, want, reportData.Findings)
	}
	var sawMissing, sawExtra bool
	for _, finding := range reportData.Findings {
		switch finding.FindingType {
		case model.FindingTypeMissing:
			sawMissing = true
		case model.FindingTypeExtra:
			sawExtra = true
		}
	}
	if !sawMissing || !sawExtra {
		t.Fatalf("expected missing and extra service account findings, got %+v", reportData.Findings)
	}
}

func TestShouldFail(t *testing.T) {
	findings := []model.Finding{
		{Severity: model.SeverityLow},
		{Severity: model.SeverityMedium},
	}

	tests := []struct {
		name   string
		failOn string
		want   bool
	}{
		{name: "never", failOn: "never", want: false},
		{name: "any", failOn: "any", want: true},
		{name: "medium", failOn: "medium", want: true},
		{name: "high without high", failOn: "high", want: false},
		{name: "case and spaces", failOn: " HIGH ", want: false},
		{name: "unknown mode", failOn: "invalid", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldFail(findings, tt.failOn); got != tt.want {
				t.Fatalf("ShouldFail(%q)=%v want=%v", tt.failOn, got, tt.want)
			}
		})
	}

	if !ShouldFail([]model.Finding{{Severity: model.SeverityHigh}}, "high") {
		t.Fatalf("high mode should fail on high severity finding")
	}
}
