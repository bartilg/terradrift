package gcp

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"terradrift/src/internal/model"

	compute "google.golang.org/api/compute/v1"
	run "google.golang.org/api/run/v2"
	secretmanager "google.golang.org/api/secretmanager/v1"
)

func TestNewObserver(t *testing.T) {
	if NewObserver() == nil {
		t.Fatalf("NewObserver returned nil")
	}
}

func TestObserveRequiresProjectBeforeCreatingClients(t *testing.T) {
	_, err := NewObserver().Observe(context.Background(), " ", model.SupportedResourceTypes())
	if err == nil {
		t.Fatalf("expected missing project error")
	}
	if !strings.Contains(err.Error(), "--project is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWantsAny(t *testing.T) {
	requested := map[string]struct{}{
		model.ResourceTypeComputeInstance: {},
	}
	if !wantsAny(requested, model.ResourceTypeBucket, model.ResourceTypeComputeInstance) {
		t.Fatalf("wantsAny should return true when any requested type matches")
	}
	if wantsAny(requested, model.ResourceTypeBucket, model.ResourceTypeServiceAccount) {
		t.Fatalf("wantsAny should return false when no requested type matches")
	}
}

func TestParseCloudRunServiceName(t *testing.T) {
	serviceID, location := parseCloudRunServiceName("projects/demo/locations/us-central1/services/api")
	if serviceID != "api" || location != "us-central1" {
		t.Fatalf("cloud run parse mismatch: serviceID=%q location=%q", serviceID, location)
	}

	serviceID, location = parseCloudRunServiceName("malformed")
	if serviceID != "malformed" || location != "" {
		t.Fatalf("malformed cloud run parse mismatch: serviceID=%q location=%q", serviceID, location)
	}
}

func TestParseArtifactRegistryRepositoryName(t *testing.T) {
	repositoryID, location := parseArtifactRegistryRepositoryName("projects/demo/locations/us/repositories/images")
	if repositoryID != "images" || location != "us" {
		t.Fatalf("artifact registry parse mismatch: repositoryID=%q location=%q", repositoryID, location)
	}

	repositoryID, location = parseArtifactRegistryRepositoryName("malformed")
	if repositoryID != "malformed" || location != "" {
		t.Fatalf("malformed artifact registry parse mismatch: repositoryID=%q location=%q", repositoryID, location)
	}
}

func TestCloudRunTemplateHelpers(t *testing.T) {
	if got := cloudRunServiceAccount(&run.GoogleCloudRunV2Service{}); got != "" {
		t.Fatalf("missing template should have empty service account, got %q", got)
	}
	if got := cloudRunContainerImage(&run.GoogleCloudRunV2Service{}); got != "" {
		t.Fatalf("missing template should have empty container image, got %q", got)
	}

	svc := &run.GoogleCloudRunV2Service{
		Template: &run.GoogleCloudRunV2RevisionTemplate{
			ServiceAccount: "app@demo.iam.gserviceaccount.com",
			Containers: []*run.GoogleCloudRunV2Container{
				{Image: "us-docker.pkg.dev/cloudrun/container/hello"},
				{Image: "ignored"},
			},
		},
	}
	if got, want := cloudRunServiceAccount(svc), "app@demo.iam.gserviceaccount.com"; got != want {
		t.Fatalf("service account mismatch: got=%q want=%q", got, want)
	}
	if got, want := cloudRunContainerImage(svc), "us-docker.pkg.dev/cloudrun/container/hello"; got != want {
		t.Fatalf("container image mismatch: got=%q want=%q", got, want)
	}
}

func TestCopyStringMap(t *testing.T) {
	in := map[string]string{"env": "dev"}
	out := copyStringMap(in)
	out["env"] = "prod"

	if got, want := in["env"], "dev"; got != want {
		t.Fatalf("copyStringMap should not alias input: got=%q want=%q", got, want)
	}
}

func TestInstanceTags(t *testing.T) {
	if got := instanceTags(&compute.Instance{}); got != nil {
		t.Fatalf("missing tags should return nil, got=%v", got)
	}

	got := instanceTags(&compute.Instance{Tags: &compute.Tags{Items: []string{"web", "ssh"}}})
	want := []string{"web", "ssh"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("instance tags mismatch: got=%v want=%v", got, want)
	}
}

func TestSortedStrings(t *testing.T) {
	input := []string{"b", "a"}
	got := sortedStrings(input)
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted strings mismatch: got=%v want=%v", got, want)
	}
	if !reflect.DeepEqual(input, []string{"b", "a"}) {
		t.Fatalf("sortedStrings should not mutate input: got=%v", input)
	}
	if got := sortedStrings(nil); !reflect.DeepEqual(got, []string{}) {
		t.Fatalf("nil input should return empty slice, got=%v", got)
	}
}

func TestSecretReplication(t *testing.T) {
	tests := []struct {
		name string
		in   *secretmanager.Replication
		want string
	}{
		{name: "nil", in: nil, want: ""},
		{name: "automatic", in: &secretmanager.Replication{Automatic: &secretmanager.Automatic{}}, want: "automatic"},
		{name: "user managed", in: &secretmanager.Replication{UserManaged: &secretmanager.UserManaged{}}, want: "user_managed"},
		{name: "empty", in: &secretmanager.Replication{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := secretReplication(tt.in); got != tt.want {
				t.Fatalf("secretReplication()=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestLastPathSegment(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "  ", want: ""},
		{in: "projects/demo/topics/events", want: "events"},
		{in: "projects/demo/topics/events/", want: "events"},
		{in: "events", want: "events"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := lastPathSegment(tt.in); got != tt.want {
				t.Fatalf("lastPathSegment(%q)=%q want=%q", tt.in, got, tt.want)
			}
		})
	}
}
