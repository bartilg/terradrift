package model

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	ResourceTypeArtifactRegistryRepository = "google_artifact_registry_repository"
	ResourceTypeBigQueryDataset            = "google_bigquery_dataset"
	ResourceTypeBucket                     = "google_storage_bucket"
	ResourceTypeCloudRunService            = "google_cloud_run_v2_service"
	ResourceTypeComputeInstance            = "google_compute_instance"
	ResourceTypeComputeNetwork             = "google_compute_network"
	ResourceTypeComputeSubnetwork          = "google_compute_subnetwork"
	ResourceTypePubSubTopic                = "google_pubsub_topic"
	ResourceTypeSecretManagerSecret        = "google_secret_manager_secret"
	ResourceTypeServiceAccount             = "google_service_account"
)

func SupportedResourceTypes() []string {
	return []string{
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
}

type FindingType string

const (
	FindingTypeMissing       FindingType = "MISSING"
	FindingTypeExtra         FindingType = "EXTRA"
	FindingTypeMismatch      FindingType = "MISMATCH"
	FindingTypeUnknownIntent FindingType = "UNKNOWN_INTENT"
)

type Severity string

const (
	SeverityLow    Severity = "LOW"
	SeverityMedium Severity = "MEDIUM"
	SeverityHigh   Severity = "HIGH"
)

type Cause string

const (
	CausePlatformDefault Cause = "PLATFORM_DEFAULT"
	CausePlatformSide    Cause = "PLATFORM_SIDE_EFFECT"
	CauseLegacyArtifact  Cause = "LEGACY_ARTIFACT"
	CauseManualChange    Cause = "MANUAL_CHANGE"
	CauseTerraformComp   Cause = "TERRAFORM_COMPUTED"
	CauseUnknown         Cause = "UNKNOWN"
)

type ExpectedResource struct {
	Address      string         `json:"address"`
	ResourceType string         `json:"resource_type"`
	Name         string         `json:"name"`
	Identity     map[string]any `json:"identity,omitempty"`
	Normalized   map[string]any `json:"normalized"`
	Raw          map[string]any `json:"raw,omitempty"`

	IdentityKnown bool            `json:"-"`
	FieldExplicit map[string]bool `json:"-"`
}

type ObservedResource struct {
	ResourceType string         `json:"resource_type"`
	ProviderID   string         `json:"provider_id"`
	Identity     map[string]any `json:"identity"`
	Normalized   map[string]any `json:"normalized"`
	Raw          map[string]any `json:"raw,omitempty"`
}

type Match struct {
	ExpectedAddress  string `json:"expected_address"`
	ObservedProvider string `json:"observed_provider_id"`
	Confidence       string `json:"confidence"`
	Reason           string `json:"reason"`
}

type DiffEntry struct {
	Field    string `json:"field"`
	Expected any    `json:"expected,omitempty"`
	Observed any    `json:"observed,omitempty"`

	ExpectedExplicit bool `json:"-"`
}

type Finding struct {
	ID               string      `json:"id"`
	ResourceType     string      `json:"resource_type"`
	FindingType      FindingType `json:"finding_type"`
	Severity         Severity    `json:"severity"`
	Cause            Cause       `json:"cause"`
	ExpectedAddress  string      `json:"expected_address,omitempty"`
	ObservedProvider string      `json:"observed_provider_id,omitempty"`
	Diff             []DiffEntry `json:"diff,omitempty"`
	Notes            []string    `json:"notes,omitempty"`
	Evidence         any         `json:"evidence,omitempty"`
}

type Metadata struct {
	Provider       string   `json:"provider"`
	Project        string   `json:"project,omitempty"`
	Path           string   `json:"path"`
	ResourceTypes  []string `json:"resource_types"`
	IgnoreDefaults bool     `json:"ignore_defaults,omitempty"`
	Version        string   `json:"version"`
}

type ScanReport struct {
	SchemaVersion     int                `json:"schema_version"`
	Metadata          Metadata           `json:"metadata"`
	ExpectedResources []ExpectedResource `json:"expected_resources"`
	ObservedResources []ObservedResource `json:"observed_resources"`
	Matches           []Match            `json:"matches"`
	Findings          []Finding          `json:"findings"`
}

func CanonicalIdentity(identity map[string]any) string {
	if len(identity) == 0 {
		return ""
	}
	keys := make([]string, 0, len(identity))
	for k := range identity {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, identity[k]))
	}
	return strings.Join(parts, "|")
}

func StableFindingID(f Finding) string {
	payload := map[string]any{
		"resource_type":        f.ResourceType,
		"finding_type":         f.FindingType,
		"expected_address":     f.ExpectedAddress,
		"observed_provider_id": f.ObservedProvider,
		"diff":                 normalizeDiffForID(f.Diff),
	}
	b, _ := json.Marshal(payload)
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:])[:12]
}

func normalizeDiffForID(diff []DiffEntry) []map[string]any {
	out := make([]map[string]any, 0, len(diff))
	for _, d := range diff {
		out = append(out, map[string]any{
			"field":    d.Field,
			"expected": d.Expected,
			"observed": d.Observed,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprintf("%v", out[i]["field"]) < fmt.Sprintf("%v", out[j]["field"])
	})
	return out
}

func SeverityRank(s Severity) int {
	switch s {
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	default:
		return 0
	}
}
