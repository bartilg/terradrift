package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"terradrift/src/internal/model"

	"cloud.google.com/go/storage"
	artifactregistry "google.golang.org/api/artifactregistry/v1"
	bigquery "google.golang.org/api/bigquery/v2"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/iam/v1"
	"google.golang.org/api/iterator"
	pubsub "google.golang.org/api/pubsub/v1"
	run "google.golang.org/api/run/v2"
	secretmanager "google.golang.org/api/secretmanager/v1"
)

type Observer interface {
	Observe(ctx context.Context, project string, resourceTypes []string) ([]model.ObservedResource, error)
}

type APIObserver struct{}

func NewObserver() *APIObserver {
	return &APIObserver{}
}

func (o *APIObserver) Observe(ctx context.Context, project string, resourceTypes []string) ([]model.ObservedResource, error) {
	if strings.TrimSpace(project) == "" {
		return nil, fmt.Errorf("--project is required for GCP observation")
	}

	requested := map[string]struct{}{}
	for _, rt := range resourceTypes {
		requested[rt] = struct{}{}
	}

	observed := make([]model.ObservedResource, 0)
	if _, ok := requested[model.ResourceTypeBucket]; ok {
		buckets, err := observeBuckets(ctx, project)
		if err != nil {
			return nil, err
		}
		observed = append(observed, buckets...)
	}

	if _, ok := requested[model.ResourceTypeServiceAccount]; ok {
		sas, err := observeServiceAccounts(ctx, project)
		if err != nil {
			return nil, err
		}
		observed = append(observed, sas...)
	}

	if _, ok := requested[model.ResourceTypeCloudRunService]; ok {
		services, err := observeCloudRunServices(ctx, project)
		if err != nil {
			return nil, err
		}
		observed = append(observed, services...)
	}

	if wantsAny(requested, model.ResourceTypeComputeInstance, model.ResourceTypeComputeNetwork, model.ResourceTypeComputeSubnetwork) {
		computeService, err := compute.NewService(ctx)
		if err != nil {
			return nil, fmt.Errorf("create compute service: %w", err)
		}

		if _, ok := requested[model.ResourceTypeComputeNetwork]; ok {
			networks, err := observeComputeNetworks(ctx, computeService, project)
			if err != nil {
				return nil, err
			}
			observed = append(observed, networks...)
		}
		if _, ok := requested[model.ResourceTypeComputeSubnetwork]; ok {
			subnetworks, err := observeComputeSubnetworks(ctx, computeService, project)
			if err != nil {
				return nil, err
			}
			observed = append(observed, subnetworks...)
		}
		if _, ok := requested[model.ResourceTypeComputeInstance]; ok {
			instances, err := observeComputeInstances(ctx, computeService, project)
			if err != nil {
				return nil, err
			}
			observed = append(observed, instances...)
		}
	}

	if _, ok := requested[model.ResourceTypePubSubTopic]; ok {
		pubsubService, err := pubsub.NewService(ctx)
		if err != nil {
			return nil, fmt.Errorf("create pubsub service: %w", err)
		}
		topics, err := observePubSubTopics(ctx, pubsubService, project)
		if err != nil {
			return nil, err
		}
		observed = append(observed, topics...)
	}

	if _, ok := requested[model.ResourceTypeBigQueryDataset]; ok {
		bigQueryService, err := bigquery.NewService(ctx)
		if err != nil {
			return nil, fmt.Errorf("create bigquery service: %w", err)
		}
		datasets, err := observeBigQueryDatasets(ctx, bigQueryService, project)
		if err != nil {
			return nil, err
		}
		observed = append(observed, datasets...)
	}

	if _, ok := requested[model.ResourceTypeArtifactRegistryRepository]; ok {
		artifactRegistryService, err := artifactregistry.NewService(ctx)
		if err != nil {
			return nil, fmt.Errorf("create artifact registry service: %w", err)
		}
		repositories, err := observeArtifactRegistryRepositories(ctx, artifactRegistryService, project)
		if err != nil {
			return nil, err
		}
		observed = append(observed, repositories...)
	}

	if _, ok := requested[model.ResourceTypeSecretManagerSecret]; ok {
		secretManagerService, err := secretmanager.NewService(ctx)
		if err != nil {
			return nil, fmt.Errorf("create secret manager service: %w", err)
		}
		secrets, err := observeSecretManagerSecrets(ctx, secretManagerService, project)
		if err != nil {
			return nil, err
		}
		observed = append(observed, secrets...)
	}

	sort.Slice(observed, func(i, j int) bool {
		if observed[i].ResourceType == observed[j].ResourceType {
			if observed[i].ProviderID == observed[j].ProviderID {
				return model.CanonicalIdentity(observed[i].Identity) < model.CanonicalIdentity(observed[j].Identity)
			}
			return observed[i].ProviderID < observed[j].ProviderID
		}
		return observed[i].ResourceType < observed[j].ResourceType
	})

	return observed, nil
}

func wantsAny(requested map[string]struct{}, resourceTypes ...string) bool {
	for _, rt := range resourceTypes {
		if _, ok := requested[rt]; ok {
			return true
		}
	}
	return false
}

func observeBuckets(ctx context.Context, project string) ([]model.ObservedResource, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create storage client: %w", err)
	}
	defer client.Close()

	it := client.Buckets(ctx, project)
	out := make([]model.ObservedResource, 0)
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list buckets: %w", err)
		}
		out = append(out, model.ObservedResource{
			ResourceType: model.ResourceTypeBucket,
			ProviderID:   fmt.Sprintf("projects/_/buckets/%s", attrs.Name),
			Identity: map[string]any{
				"name": attrs.Name,
			},
			Normalized: map[string]any{
				"name":                        attrs.Name,
				"location":                    attrs.Location,
				"storage_class":               attrs.StorageClass,
				"uniform_bucket_level_access": attrs.UniformBucketLevelAccess.Enabled,
				"versioning_enabled":          attrs.VersioningEnabled,
				"labels":                      copyStringMap(attrs.Labels),
			},
		})
	}
	return out, nil
}

func observeServiceAccounts(ctx context.Context, project string) ([]model.ObservedResource, error) {
	iamService, err := iam.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("create iam service: %w", err)
	}

	parent := fmt.Sprintf("projects/%s", project)
	call := iamService.Projects.ServiceAccounts.List(parent)
	out := make([]model.ObservedResource, 0)
	err = call.Pages(ctx, func(page *iam.ListServiceAccountsResponse) error {
		for _, sa := range page.Accounts {
			accountID := serviceAccountIDFromEmail(sa.Email)
			out = append(out, model.ObservedResource{
				ResourceType: model.ResourceTypeServiceAccount,
				ProviderID:   sa.Name,
				Identity: map[string]any{
					"email": sa.Email,
				},
				Normalized: map[string]any{
					"account_id":   accountID,
					"project":      project,
					"display_name": sa.DisplayName,
					"description":  sa.Description,
					"disabled":     sa.Disabled,
				},
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list service accounts: %w", err)
	}
	return out, nil
}

func serviceAccountIDFromEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func observeCloudRunServices(ctx context.Context, project string) ([]model.ObservedResource, error) {
	runService, err := run.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("create cloud run service client: %w", err)
	}

	parent := fmt.Sprintf("projects/%s/locations/-", project)
	call := runService.Projects.Locations.Services.List(parent)
	out := make([]model.ObservedResource, 0)
	err = call.Pages(ctx, func(page *run.GoogleCloudRunV2ListServicesResponse) error {
		for _, svc := range page.Services {
			serviceID, location := parseCloudRunServiceName(svc.Name)
			out = append(out, model.ObservedResource{
				ResourceType: model.ResourceTypeCloudRunService,
				ProviderID:   svc.Name,
				Identity: map[string]any{
					"name":     serviceID,
					"location": location,
				},
				Normalized: map[string]any{
					"name":            serviceID,
					"location":        location,
					"ingress":         svc.Ingress,
					"labels":          copyStringMap(svc.Labels),
					"service_account": cloudRunServiceAccount(svc),
					"container_image": cloudRunContainerImage(svc),
				},
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list cloud run services: %w", err)
	}
	return out, nil
}

func observeComputeNetworks(ctx context.Context, computeService *compute.Service, project string) ([]model.ObservedResource, error) {
	call := computeService.Networks.List(project)
	out := make([]model.ObservedResource, 0)
	err := call.Pages(ctx, func(page *compute.NetworkList) error {
		for _, network := range page.Items {
			if network == nil {
				continue
			}
			routingMode := ""
			if network.RoutingConfig != nil {
				routingMode = network.RoutingConfig.RoutingMode
			}
			out = append(out, model.ObservedResource{
				ResourceType: model.ResourceTypeComputeNetwork,
				ProviderID:   network.SelfLink,
				Identity: map[string]any{
					"name": network.Name,
				},
				Normalized: map[string]any{
					"name":                    network.Name,
					"auto_create_subnetworks": network.AutoCreateSubnetworks,
					"routing_mode":            routingMode,
				},
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list compute networks: %w", err)
	}
	return out, nil
}

func observeComputeSubnetworks(ctx context.Context, computeService *compute.Service, project string) ([]model.ObservedResource, error) {
	call := computeService.Subnetworks.AggregatedList(project)
	out := make([]model.ObservedResource, 0)
	err := call.Pages(ctx, func(page *compute.SubnetworkAggregatedList) error {
		for _, scopedList := range page.Items {
			for _, subnet := range scopedList.Subnetworks {
				if subnet == nil {
					continue
				}
				region := lastPathSegment(subnet.Region)
				out = append(out, model.ObservedResource{
					ResourceType: model.ResourceTypeComputeSubnetwork,
					ProviderID:   subnet.SelfLink,
					Identity: map[string]any{
						"name":   subnet.Name,
						"region": region,
					},
					Normalized: map[string]any{
						"name":                     subnet.Name,
						"network":                  lastPathSegment(subnet.Network),
						"region":                   region,
						"ip_cidr_range":            subnet.IpCidrRange,
						"private_ip_google_access": subnet.PrivateIpGoogleAccess,
					},
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list compute subnetworks: %w", err)
	}
	return out, nil
}

func observeComputeInstances(ctx context.Context, computeService *compute.Service, project string) ([]model.ObservedResource, error) {
	call := computeService.Instances.AggregatedList(project)
	out := make([]model.ObservedResource, 0)
	err := call.Pages(ctx, func(page *compute.InstanceAggregatedList) error {
		for _, scopedList := range page.Items {
			for _, instance := range scopedList.Instances {
				if instance == nil {
					continue
				}
				zone := lastPathSegment(instance.Zone)
				out = append(out, model.ObservedResource{
					ResourceType: model.ResourceTypeComputeInstance,
					ProviderID:   instance.SelfLink,
					Identity: map[string]any{
						"name": instance.Name,
						"zone": zone,
					},
					Normalized: map[string]any{
						"name":         instance.Name,
						"zone":         zone,
						"machine_type": lastPathSegment(instance.MachineType),
						"labels":       copyStringMap(instance.Labels),
						"tags":         sortedStrings(instanceTags(instance)),
					},
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list compute instances: %w", err)
	}
	return out, nil
}

func observePubSubTopics(ctx context.Context, pubsubService *pubsub.Service, project string) ([]model.ObservedResource, error) {
	call := pubsubService.Projects.Topics.List(fmt.Sprintf("projects/%s", project))
	out := make([]model.ObservedResource, 0)
	err := call.Pages(ctx, func(page *pubsub.ListTopicsResponse) error {
		for _, topic := range page.Topics {
			if topic == nil {
				continue
			}
			out = append(out, model.ObservedResource{
				ResourceType: model.ResourceTypePubSubTopic,
				ProviderID:   topic.Name,
				Identity: map[string]any{
					"name": lastPathSegment(topic.Name),
				},
				Normalized: map[string]any{
					"name":   lastPathSegment(topic.Name),
					"labels": copyStringMap(topic.Labels),
				},
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list pubsub topics: %w", err)
	}
	return out, nil
}

func observeBigQueryDatasets(ctx context.Context, bigQueryService *bigquery.Service, project string) ([]model.ObservedResource, error) {
	call := bigQueryService.Datasets.List(project)
	out := make([]model.ObservedResource, 0)
	err := call.Pages(ctx, func(page *bigquery.DatasetList) error {
		for _, dataset := range page.Datasets {
			if dataset == nil || dataset.DatasetReference == nil {
				continue
			}
			datasetID := dataset.DatasetReference.DatasetId
			providerID := fmt.Sprintf("projects/%s/datasets/%s", project, datasetID)
			if dataset.Id != "" {
				providerID = dataset.Id
			}
			out = append(out, model.ObservedResource{
				ResourceType: model.ResourceTypeBigQueryDataset,
				ProviderID:   providerID,
				Identity: map[string]any{
					"dataset_id": datasetID,
				},
				Normalized: map[string]any{
					"dataset_id":    datasetID,
					"location":      dataset.Location,
					"friendly_name": dataset.FriendlyName,
					"labels":        copyStringMap(dataset.Labels),
				},
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list bigquery datasets: %w", err)
	}
	return out, nil
}

func observeArtifactRegistryRepositories(ctx context.Context, artifactRegistryService *artifactregistry.Service, project string) ([]model.ObservedResource, error) {
	locationsCall := artifactRegistryService.Projects.Locations.List(fmt.Sprintf("projects/%s", project))
	out := make([]model.ObservedResource, 0)
	err := locationsCall.Pages(ctx, func(page *artifactregistry.ListLocationsResponse) error {
		for _, location := range page.Locations {
			if location == nil || strings.TrimSpace(location.Name) == "" {
				continue
			}
			repositoriesCall := artifactRegistryService.Projects.Locations.Repositories.List(location.Name)
			if err := repositoriesCall.Pages(ctx, func(repoPage *artifactregistry.ListRepositoriesResponse) error {
				for _, repository := range repoPage.Repositories {
					if repository == nil {
						continue
					}
					repositoryID, repositoryLocation := parseArtifactRegistryRepositoryName(repository.Name)
					out = append(out, model.ObservedResource{
						ResourceType: model.ResourceTypeArtifactRegistryRepository,
						ProviderID:   repository.Name,
						Identity: map[string]any{
							"repository_id": repositoryID,
							"location":      repositoryLocation,
						},
						Normalized: map[string]any{
							"repository_id": repositoryID,
							"location":      repositoryLocation,
							"format":        repository.Format,
							"description":   repository.Description,
							"labels":        copyStringMap(repository.Labels),
						},
					})
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list artifact registry repositories: %w", err)
	}
	return out, nil
}

func observeSecretManagerSecrets(ctx context.Context, secretManagerService *secretmanager.Service, project string) ([]model.ObservedResource, error) {
	call := secretManagerService.Projects.Secrets.List(fmt.Sprintf("projects/%s", project))
	out := make([]model.ObservedResource, 0)
	err := call.Pages(ctx, func(page *secretmanager.ListSecretsResponse) error {
		for _, secret := range page.Secrets {
			if secret == nil {
				continue
			}
			out = append(out, model.ObservedResource{
				ResourceType: model.ResourceTypeSecretManagerSecret,
				ProviderID:   secret.Name,
				Identity: map[string]any{
					"secret_id": lastPathSegment(secret.Name),
				},
				Normalized: map[string]any{
					"secret_id":   lastPathSegment(secret.Name),
					"labels":      copyStringMap(secret.Labels),
					"replication": secretReplication(secret.Replication),
				},
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list secret manager secrets: %w", err)
	}
	return out, nil
}

func parseCloudRunServiceName(providerName string) (serviceID string, location string) {
	parts := strings.Split(providerName, "/")
	if len(parts) >= 6 {
		return parts[5], parts[3]
	}
	return providerName, ""
}

func parseArtifactRegistryRepositoryName(providerName string) (repositoryID string, location string) {
	parts := strings.Split(providerName, "/")
	if len(parts) >= 6 {
		return parts[5], parts[3]
	}
	return providerName, ""
}

func cloudRunServiceAccount(svc *run.GoogleCloudRunV2Service) string {
	if svc.Template == nil {
		return ""
	}
	return svc.Template.ServiceAccount
}

func cloudRunContainerImage(svc *run.GoogleCloudRunV2Service) string {
	if svc.Template == nil || len(svc.Template.Containers) == 0 {
		return ""
	}
	return svc.Template.Containers[0].Image
}

func copyStringMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func instanceTags(instance *compute.Instance) []string {
	if instance.Tags == nil {
		return nil
	}
	out := make([]string, 0, len(instance.Tags.Items))
	out = append(out, instance.Tags.Items...)
	return out
}

func sortedStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}

func secretReplication(replication *secretmanager.Replication) string {
	if replication == nil {
		return ""
	}
	if replication.Automatic != nil {
		return "automatic"
	}
	if replication.UserManaged != nil {
		return "user_managed"
	}
	return ""
}

func lastPathSegment(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	parts := strings.Split(strings.TrimRight(v, "/"), "/")
	return parts[len(parts)-1]
}
