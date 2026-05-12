package model

import (
	"reflect"
	"testing"
)

func TestSupportedResourceTypesReturnsAllKnownGCPTypes(t *testing.T) {
	want := []string{
		ResourceTypeArtifactRegistryRepository,
		ResourceTypeBigQueryDataset,
		ResourceTypeBucket,
		ResourceTypeCloudRunService,
		ResourceTypeComputeInstance,
		ResourceTypeComputeNetwork,
		ResourceTypeComputeSubnetwork,
		ResourceTypePubSubTopic,
		ResourceTypeSecretManagerSecret,
		ResourceTypeServiceAccount,
	}

	got := SupportedResourceTypes()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("supported resource types mismatch:\ngot=%v\nwant=%v", got, want)
	}

	seen := map[string]struct{}{}
	for _, rt := range got {
		if rt == "" {
			t.Fatalf("supported resource type should not be empty")
		}
		if _, ok := seen[rt]; ok {
			t.Fatalf("duplicate supported resource type: %s", rt)
		}
		seen[rt] = struct{}{}
	}
}

func TestSupportedResourceTypesReturnsCopy(t *testing.T) {
	first := SupportedResourceTypes()
	first[0] = "mutated"

	second := SupportedResourceTypes()
	if second[0] == "mutated" {
		t.Fatalf("SupportedResourceTypes should return a fresh slice")
	}
}

func TestCanonicalIdentitySortsKeys(t *testing.T) {
	got := CanonicalIdentity(map[string]any{
		"region": "us-central1",
		"name":   "default",
	})
	want := "name=default|region=us-central1"
	if got != want {
		t.Fatalf("canonical identity mismatch: got=%q want=%q", got, want)
	}
}

func TestCanonicalIdentityEmpty(t *testing.T) {
	if got := CanonicalIdentity(nil); got != "" {
		t.Fatalf("nil identity should be empty, got %q", got)
	}
	if got := CanonicalIdentity(map[string]any{}); got != "" {
		t.Fatalf("empty identity should be empty, got %q", got)
	}
}

func TestStableFindingIDIgnoresDiffOrder(t *testing.T) {
	base := Finding{
		ResourceType:     ResourceTypeBucket,
		FindingType:      FindingTypeMismatch,
		ExpectedAddress:  "google_storage_bucket.logs",
		ObservedProvider: "projects/_/buckets/logs",
		Diff: []DiffEntry{
			{Field: "storage_class", Expected: "STANDARD", Observed: "NEARLINE"},
			{Field: "location", Expected: "US", Observed: "EU"},
		},
	}
	reordered := base
	reordered.Diff = []DiffEntry{
		{Field: "location", Expected: "US", Observed: "EU"},
		{Field: "storage_class", Expected: "STANDARD", Observed: "NEARLINE"},
	}

	if got, want := StableFindingID(base), StableFindingID(reordered); got != want {
		t.Fatalf("stable finding id should ignore diff order: got=%s want=%s", got, want)
	}
}

func TestStableFindingIDChangesForFindingIdentity(t *testing.T) {
	first := Finding{
		ResourceType:    ResourceTypeBucket,
		FindingType:     FindingTypeMissing,
		ExpectedAddress: "google_storage_bucket.logs",
	}
	second := first
	second.ExpectedAddress = "google_storage_bucket.assets"

	if got, other := StableFindingID(first), StableFindingID(second); got == other {
		t.Fatalf("stable finding id should change for different finding identity: got both %s", got)
	}
}

func TestSeverityRank(t *testing.T) {
	tests := []struct {
		name string
		in   Severity
		want int
	}{
		{name: "high", in: SeverityHigh, want: 3},
		{name: "medium", in: SeverityMedium, want: 2},
		{name: "low", in: SeverityLow, want: 1},
		{name: "unknown", in: Severity("UNKNOWN"), want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SeverityRank(tt.in); got != tt.want {
				t.Fatalf("SeverityRank(%q)=%d want=%d", tt.in, got, tt.want)
			}
		})
	}
}
