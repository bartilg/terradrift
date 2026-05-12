package terraform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"terradrift/src/internal/model"
)

func TestParseExpectedExtractsLiteralAndUnknownIdentities(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "fixtures", "basic")
	resources, err := ParseExpected(root, "demo-project", []string{model.ResourceTypeBucket, model.ResourceTypeServiceAccount})
	if err != nil {
		t.Fatalf("ParseExpected failed: %v", err)
	}
	if got, want := len(resources), 3; got != want {
		t.Fatalf("resource count mismatch: got=%d want=%d", got, want)
	}

	bucket := findByAddress(resources, "google_storage_bucket.logs")
	if bucket == nil {
		t.Fatalf("bucket resource not found")
	}
	if !bucket.IdentityKnown {
		t.Fatalf("bucket identity should be known")
	}
	if got, want := bucket.Identity["name"], any("td-logs-bucket"); got != want {
		t.Fatalf("bucket identity mismatch: got=%v want=%v", got, want)
	}
	if got, want := bucket.Normalized["versioning_enabled"], any(true); got != want {
		t.Fatalf("bucket versioning mismatch: got=%v want=%v", got, want)
	}

	sa := findByAddress(resources, "google_service_account.app")
	if sa == nil {
		t.Fatalf("service account resource not found")
	}
	if !sa.IdentityKnown {
		t.Fatalf("service account identity should be known")
	}
	if got, want := sa.Identity["email"], any("app-sa@demo-project.iam.gserviceaccount.com"); got != want {
		t.Fatalf("service account email mismatch: got=%v want=%v", got, want)
	}
	if got, want := sa.Normalized["display_name"], any("App SA"); got != want {
		t.Fatalf("service account display_name mismatch: got=%v want=%v", got, want)
	}

	computed := findByAddress(resources, "google_service_account.computed")
	if computed == nil {
		t.Fatalf("computed service account not found")
	}
	if computed.IdentityKnown {
		t.Fatalf("computed service account identity should be unknown")
	}
	if _, ok := computed.Identity["email"]; ok {
		t.Fatalf("computed service account should not have email identity")
	}
	if _, ok := computed.Normalized["account_id"]; ok {
		t.Fatalf("computed account_id must not be guessed into normalized fields")
	}
}

func TestParseExpectedServiceAccountUsesLiteralProjectForIdentity(t *testing.T) {
	root := t.TempDir()
	tf := []byte(`
resource "google_service_account" "app" {
  project      = "other-project"
  account_id   = "app-sa"
  display_name = "App SA"
}
`)
	if err := os.WriteFile(filepath.Join(root, "main.tf"), tf, 0o644); err != nil {
		t.Fatalf("write fixture failed: %v", err)
	}

	resources, err := ParseExpected(root, "scan-project", []string{model.ResourceTypeServiceAccount})
	if err != nil {
		t.Fatalf("ParseExpected failed: %v", err)
	}
	if got, want := len(resources), 1; got != want {
		t.Fatalf("resource count mismatch: got=%d want=%d", got, want)
	}

	sa := resources[0]
	if !sa.IdentityKnown {
		t.Fatalf("service account identity should be known")
	}
	if got, want := sa.Identity["email"], any("app-sa@other-project.iam.gserviceaccount.com"); got != want {
		t.Fatalf("service account email mismatch: got=%v want=%v", got, want)
	}
	if got, want := sa.Normalized["project"], any("other-project"); got != want {
		t.Fatalf("service account project normalization mismatch: got=%v want=%v", got, want)
	}
}

func TestParseExpectedServiceAccountUsesProviderProjectVariableForIdentity(t *testing.T) {
	root := t.TempDir()
	tf := []byte(`
variable "project_id" {
  type = string
}

provider "google" {
  project = var.project_id
}

resource "google_service_account" "app" {
  account_id   = "app-sa"
  display_name = "App SA"
}
`)
	if err := os.WriteFile(filepath.Join(root, "main.tf"), tf, 0o644); err != nil {
		t.Fatalf("write fixture failed: %v", err)
	}
	tfvars := []byte(`project_id = "940404789850"`)
	if err := os.WriteFile(filepath.Join(root, "terraform.tfvars"), tfvars, 0o644); err != nil {
		t.Fatalf("write tfvars failed: %v", err)
	}

	resources, err := ParseExpected(root, "powerful-axon-480921-k8", []string{model.ResourceTypeServiceAccount})
	if err != nil {
		t.Fatalf("ParseExpected failed: %v", err)
	}
	if got, want := len(resources), 1; got != want {
		t.Fatalf("resource count mismatch: got=%d want=%d", got, want)
	}

	sa := resources[0]
	if !sa.IdentityKnown {
		t.Fatalf("service account identity should be known")
	}
	if got, want := sa.Identity["email"], any("app-sa@940404789850.iam.gserviceaccount.com"); got != want {
		t.Fatalf("service account email mismatch: got=%v want=%v", got, want)
	}
}

func TestParseExpectedResolvesVariablesAndLocalsForResourceIdentities(t *testing.T) {
	root := t.TempDir()
	tf := []byte(`
variable "project_id" {
  type = string
}

variable "region" {
  type = string
}

variable "zone" {
  type = string
}

locals {
  sample_labels = {
    managed_by = "terradrift"
  }
}

resource "google_storage_bucket" "sample" {
  name   = "${var.project_id}-terradrift-sample-bucket"
  labels = local.sample_labels
}

resource "google_compute_subnetwork" "sample" {
  name   = "td-sample-subnet"
  region = var.region
}

resource "google_compute_instance" "sample" {
  name   = "td-sample-vm"
  zone   = var.zone
  labels = local.sample_labels
}

resource "google_artifact_registry_repository" "containers" {
  location      = var.region
  repository_id = "td-sample-repo"
}
`)
	if err := os.WriteFile(filepath.Join(root, "main.tf"), tf, 0o644); err != nil {
		t.Fatalf("write fixture failed: %v", err)
	}
	tfvars := []byte(`
project_id = "940404789850"
region     = "us-central1"
zone       = "us-central1-a"
`)
	if err := os.WriteFile(filepath.Join(root, "terraform.tfvars"), tfvars, 0o644); err != nil {
		t.Fatalf("write tfvars failed: %v", err)
	}

	resources, err := ParseExpected(root, "powerful-axon-480921-k8", []string{
		model.ResourceTypeArtifactRegistryRepository,
		model.ResourceTypeBucket,
		model.ResourceTypeComputeInstance,
		model.ResourceTypeComputeSubnetwork,
	})
	if err != nil {
		t.Fatalf("ParseExpected failed: %v", err)
	}

	bucket := findByAddress(resources, "google_storage_bucket.sample")
	if bucket == nil || !bucket.IdentityKnown {
		t.Fatalf("bucket identity should be known: %+v", bucket)
	}
	if got, want := bucket.Identity["name"], any("940404789850-terradrift-sample-bucket"); got != want {
		t.Fatalf("bucket identity mismatch: got=%v want=%v", got, want)
	}
	if got, want := bucket.Normalized["labels"], map[string]string{"managed_by": "terradrift"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bucket labels mismatch: got=%v want=%v", got, want)
	}

	subnet := findByAddress(resources, "google_compute_subnetwork.sample")
	if subnet == nil || !subnet.IdentityKnown {
		t.Fatalf("subnetwork identity should be known: %+v", subnet)
	}
	if got, want := subnet.Identity["region"], any("us-central1"); got != want {
		t.Fatalf("subnetwork region mismatch: got=%v want=%v", got, want)
	}

	instance := findByAddress(resources, "google_compute_instance.sample")
	if instance == nil || !instance.IdentityKnown {
		t.Fatalf("instance identity should be known: %+v", instance)
	}
	if got, want := instance.Identity["zone"], any("us-central1-a"); got != want {
		t.Fatalf("instance zone mismatch: got=%v want=%v", got, want)
	}

	repo := findByAddress(resources, "google_artifact_registry_repository.containers")
	if repo == nil || !repo.IdentityKnown {
		t.Fatalf("artifact registry repository identity should be known: %+v", repo)
	}
	if got, want := repo.Identity["location"], any("us-central1"); got != want {
		t.Fatalf("artifact registry location mismatch: got=%v want=%v", got, want)
	}
}

func TestParseExpectedResolvesPureHCLFunctionsForResourceIdentities(t *testing.T) {
	root := t.TempDir()
	tf := []byte(`
variable "project_id" {
  type = string
}

variable "region" {
  type = string
}

variable "suffix" {
  type = string
}

locals {
  normalized_suffix = lower(replace(var.suffix, "_", "-"))
  sample_labels = merge(
    { managed_by = "terradrift" },
    { sample = "true" },
  )
}

resource "google_storage_bucket" "sample" {
  name   = format("%s-%s-bucket", var.project_id, local.normalized_suffix)
  labels = local.sample_labels
}

resource "google_compute_subnetwork" "sample" {
  name   = format("td-%s-subnet", substr(local.normalized_suffix, 0, 6))
  region = coalesce(var.region, "us-east1")
}

resource "google_compute_instance" "sample" {
  name = format("td-%s-vm", substr(local.normalized_suffix, 0, 6))
  zone = format("%s-a", var.region)
}

resource "google_artifact_registry_repository" "containers" {
  location      = trimspace(" us-central1 ")
  repository_id = format("td-%s-repo", substr(local.normalized_suffix, 0, 6))
}
`)
	if err := os.WriteFile(filepath.Join(root, "main.tf"), tf, 0o644); err != nil {
		t.Fatalf("write fixture failed: %v", err)
	}
	tfvars := []byte(`
project_id = "940404789850"
region     = "us-central1"
suffix     = "Sample_App"
`)
	if err := os.WriteFile(filepath.Join(root, "terraform.tfvars"), tfvars, 0o644); err != nil {
		t.Fatalf("write tfvars failed: %v", err)
	}

	resources, err := ParseExpected(root, "powerful-axon-480921-k8", []string{
		model.ResourceTypeArtifactRegistryRepository,
		model.ResourceTypeBucket,
		model.ResourceTypeComputeInstance,
		model.ResourceTypeComputeSubnetwork,
	})
	if err != nil {
		t.Fatalf("ParseExpected failed: %v", err)
	}

	bucket := findByAddress(resources, "google_storage_bucket.sample")
	if bucket == nil || !bucket.IdentityKnown {
		t.Fatalf("bucket identity should be known: %+v", bucket)
	}
	if got, want := bucket.Identity["name"], any("940404789850-sample-app-bucket"); got != want {
		t.Fatalf("bucket identity mismatch: got=%v want=%v", got, want)
	}
	if got, want := bucket.Normalized["labels"], map[string]string{"managed_by": "terradrift", "sample": "true"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bucket labels mismatch: got=%v want=%v", got, want)
	}

	subnet := findByAddress(resources, "google_compute_subnetwork.sample")
	if subnet == nil || !subnet.IdentityKnown {
		t.Fatalf("subnetwork identity should be known: %+v", subnet)
	}
	if got, want := subnet.Identity["name"], any("td-sample-subnet"); got != want {
		t.Fatalf("subnetwork name mismatch: got=%v want=%v", got, want)
	}
	if got, want := subnet.Identity["region"], any("us-central1"); got != want {
		t.Fatalf("subnetwork region mismatch: got=%v want=%v", got, want)
	}

	instance := findByAddress(resources, "google_compute_instance.sample")
	if instance == nil || !instance.IdentityKnown {
		t.Fatalf("instance identity should be known: %+v", instance)
	}
	if got, want := instance.Identity["name"], any("td-sample-vm"); got != want {
		t.Fatalf("instance name mismatch: got=%v want=%v", got, want)
	}
	if got, want := instance.Identity["zone"], any("us-central1-a"); got != want {
		t.Fatalf("instance zone mismatch: got=%v want=%v", got, want)
	}

	repo := findByAddress(resources, "google_artifact_registry_repository.containers")
	if repo == nil || !repo.IdentityKnown {
		t.Fatalf("artifact registry repository identity should be known: %+v", repo)
	}
	if got, want := repo.Identity["repository_id"], any("td-sample-repo"); got != want {
		t.Fatalf("artifact registry repository_id mismatch: got=%v want=%v", got, want)
	}
	if got, want := repo.Identity["location"], any("us-central1"); got != want {
		t.Fatalf("artifact registry location mismatch: got=%v want=%v", got, want)
	}
}

func TestParseExpectedNormalizationStable(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "fixtures", "basic")
	first, err := ParseExpected(root, "demo-project", []string{model.ResourceTypeBucket, model.ResourceTypeServiceAccount})
	if err != nil {
		t.Fatalf("first ParseExpected failed: %v", err)
	}
	second, err := ParseExpected(root, "demo-project", []string{model.ResourceTypeBucket, model.ResourceTypeServiceAccount})
	if err != nil {
		t.Fatalf("second ParseExpected failed: %v", err)
	}

	b1, _ := json.Marshal(first)
	b2, _ := json.Marshal(second)
	if !reflect.DeepEqual(b1, b2) {
		t.Fatalf("normalized output is not stable across parses")
	}
}

func TestParseExpectedCloudRunLiteralIdentity(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "fixtures", "cloudrun")
	resources, err := ParseExpected(root, "demo-project", []string{model.ResourceTypeCloudRunService})
	if err != nil {
		t.Fatalf("ParseExpected failed: %v", err)
	}
	if got, want := len(resources), 2; got != want {
		t.Fatalf("resource count mismatch: got=%d want=%d", got, want)
	}

	demo := findByAddress(resources, "google_cloud_run_v2_service.demo")
	if demo == nil {
		t.Fatalf("cloud run demo resource not found")
	}
	if !demo.IdentityKnown {
		t.Fatalf("cloud run demo identity should be known")
	}
	if got, want := demo.Identity["name"], any("demo-serverless-service"); got != want {
		t.Fatalf("cloud run name mismatch: got=%v want=%v", got, want)
	}
	if got, want := demo.Identity["location"], any("us-central1"); got != want {
		t.Fatalf("cloud run location mismatch: got=%v want=%v", got, want)
	}
	if got, want := demo.Normalized["container_image"], any("us-docker.pkg.dev/cloudrun/container/hello"); got != want {
		t.Fatalf("cloud run image mismatch: got=%v want=%v", got, want)
	}
}

func TestParseExpectedCloudRunVariableDefaultLocationIdentity(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "fixtures", "cloudrun")
	resources, err := ParseExpected(root, "demo-project", []string{model.ResourceTypeCloudRunService})
	if err != nil {
		t.Fatalf("ParseExpected failed: %v", err)
	}

	computed := findByAddress(resources, "google_cloud_run_v2_service.computed_location")
	if computed == nil {
		t.Fatalf("computed cloud run resource not found")
	}
	if !computed.IdentityKnown {
		t.Fatalf("computed cloud run identity should be known")
	}
	if got, want := computed.Identity["name"], any("computed-service"); got != want {
		t.Fatalf("computed cloud run name mismatch: got=%v want=%v", got, want)
	}
	if got, want := computed.Identity["location"], any("us-central1"); got != want {
		t.Fatalf("computed cloud run location mismatch: got=%v want=%v", got, want)
	}
}

func TestParseExpectedAdditionalGCPResources(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "fixtures", "additional")
	resourceTypes := []string{
		model.ResourceTypeArtifactRegistryRepository,
		model.ResourceTypeBigQueryDataset,
		model.ResourceTypeComputeInstance,
		model.ResourceTypeComputeNetwork,
		model.ResourceTypeComputeSubnetwork,
		model.ResourceTypePubSubTopic,
		model.ResourceTypeSecretManagerSecret,
	}
	resources, err := ParseExpected(root, "demo-project", resourceTypes)
	if err != nil {
		t.Fatalf("ParseExpected failed: %v", err)
	}
	if got, want := len(resources), 7; got != want {
		t.Fatalf("resource count mismatch: got=%d want=%d", got, want)
	}

	network := findByAddress(resources, "google_compute_network.sample")
	if network == nil || !network.IdentityKnown {
		t.Fatalf("compute network should have known identity")
	}
	if got, want := network.Normalized["routing_mode"], any("REGIONAL"); got != want {
		t.Fatalf("compute network routing_mode mismatch: got=%v want=%v", got, want)
	}

	subnetwork := findByAddress(resources, "google_compute_subnetwork.sample")
	if subnetwork == nil || !subnetwork.IdentityKnown {
		t.Fatalf("compute subnetwork should have known identity")
	}
	if got, want := subnetwork.Identity["region"], any("us-central1"); got != want {
		t.Fatalf("compute subnetwork region mismatch: got=%v want=%v", got, want)
	}

	instance := findByAddress(resources, "google_compute_instance.sample")
	if instance == nil || !instance.IdentityKnown {
		t.Fatalf("compute instance should have known identity")
	}
	if got, want := instance.Normalized["machine_type"], any("e2-micro"); got != want {
		t.Fatalf("compute instance machine_type mismatch: got=%v want=%v", got, want)
	}

	topic := findByAddress(resources, "google_pubsub_topic.events")
	if topic == nil || !topic.IdentityKnown {
		t.Fatalf("pubsub topic should have known identity")
	}
	if got, want := topic.Identity["name"], any("td-events-topic"); got != want {
		t.Fatalf("pubsub topic identity mismatch: got=%v want=%v", got, want)
	}

	dataset := findByAddress(resources, "google_bigquery_dataset.analytics")
	if dataset == nil || !dataset.IdentityKnown {
		t.Fatalf("bigquery dataset should have known identity")
	}
	if got, want := dataset.Normalized["location"], any("US"); got != want {
		t.Fatalf("bigquery dataset location mismatch: got=%v want=%v", got, want)
	}

	repository := findByAddress(resources, "google_artifact_registry_repository.containers")
	if repository == nil || !repository.IdentityKnown {
		t.Fatalf("artifact registry repository should have known identity")
	}
	if got, want := repository.Identity["location"], any("us-central1"); got != want {
		t.Fatalf("artifact registry location mismatch: got=%v want=%v", got, want)
	}

	secret := findByAddress(resources, "google_secret_manager_secret.app")
	if secret == nil || !secret.IdentityKnown {
		t.Fatalf("secret manager secret should have known identity")
	}
	if got, want := secret.Normalized["replication"], any("automatic"); got != want {
		t.Fatalf("secret replication mismatch: got=%v want=%v", got, want)
	}
}

func TestParseExpectedCoversEverySupportedResourceType(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "fixtures")
	resources, err := ParseExpected(root, "demo-project", model.SupportedResourceTypes())
	if err != nil {
		t.Fatalf("ParseExpected failed: %v", err)
	}

	seen := map[string]struct{}{}
	for _, resource := range resources {
		seen[resource.ResourceType] = struct{}{}
	}
	for _, resourceType := range model.SupportedResourceTypes() {
		if _, ok := seen[resourceType]; !ok {
			t.Fatalf("supported resource type %s has no parser fixture coverage", resourceType)
		}
	}
}

func TestParseExpectedPerfFixtureSizes(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{name: "10-resources", want: 10},
		{name: "25-resources", want: 25},
		{name: "50-resources", want: 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join("..", "..", "..", "testdata", "fixtures", "perf", tt.name)
			resources, err := ParseExpected(root, "demo-project", []string{model.ResourceTypeServiceAccount})
			if err != nil {
				t.Fatalf("ParseExpected failed: %v", err)
			}
			if got := len(resources); got != tt.want {
				t.Fatalf("resource count mismatch: got=%d want=%d", got, tt.want)
			}
			for _, resource := range resources {
				if !resource.IdentityKnown {
					t.Fatalf("resource %s should have known identity", resource.Address)
				}
			}
		})
	}
}

func findByAddress(resources []model.ExpectedResource, address string) *model.ExpectedResource {
	for i := range resources {
		if resources[i].Address == address {
			return &resources[i]
		}
	}
	return nil
}
