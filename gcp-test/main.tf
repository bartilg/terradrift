terraform {
  required_version = ">= 1.14.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 7.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

locals {
  sample_labels = {
    managed_by = "terradrift"
    sample     = "true"
  }
}

resource "google_project_service" "apis" {
  for_each = toset([
    "artifactregistry.googleapis.com",
    "bigquery.googleapis.com",
    "compute.googleapis.com",
    "iam.googleapis.com",
    "pubsub.googleapis.com",
    "run.googleapis.com",
    "secretmanager.googleapis.com",
    "storage.googleapis.com",
  ])

  project            = var.project_id
  service            = each.value
  disable_on_destroy = false
}

resource "google_storage_bucket" "sample" {
  name                        = "${var.project_id}-terradrift-sample-bucket"
  location                    = "US"
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true

  labels = local.sample_labels

  versioning {
    enabled = true
  }

  depends_on = [google_project_service.apis["storage.googleapis.com"]]
}

resource "google_service_account" "app" {
  account_id   = "td-sample-app"
  display_name = "Terradrift Sample App"
  description  = "Managed by the gcp-test Terradrift sample"

  depends_on = [google_project_service.apis["iam.googleapis.com"]]
}

resource "google_compute_network" "sample" {
  name                    = "td-sample-network"
  auto_create_subnetworks = false
  routing_mode            = "REGIONAL"

  depends_on = [google_project_service.apis["compute.googleapis.com"]]
}

resource "google_compute_subnetwork" "sample" {
  name                     = "td-sample-subnet"
  region                   = var.region
  ip_cidr_range            = "10.42.0.0/24"
  private_ip_google_access = true
  network                  = google_compute_network.sample.self_link

  depends_on = [google_project_service.apis["compute.googleapis.com"]]
}

resource "google_compute_instance" "sample" {
  name         = "td-sample-vm"
  machine_type = "e2-micro"
  zone         = var.zone

  labels = local.sample_labels
  tags   = ["sample", "terradrift"]

  boot_disk {
    initialize_params {
      image = "projects/debian-cloud/global/images/family/debian-12"
      size  = 10
      type  = "pd-balanced"
    }
  }

  network_interface {
    subnetwork = google_compute_subnetwork.sample.self_link
  }

  metadata = {
    enable-oslogin = "TRUE"
  }

  depends_on = [google_project_service.apis["compute.googleapis.com"]]
}

resource "google_pubsub_topic" "events" {
  name   = "td-events-topic"
  labels = local.sample_labels

  depends_on = [google_project_service.apis["pubsub.googleapis.com"]]
}

resource "google_bigquery_dataset" "analytics" {
  dataset_id    = "td_analytics"
  location      = "US"
  friendly_name = "Terradrift Analytics"
  labels        = local.sample_labels

  depends_on = [google_project_service.apis["bigquery.googleapis.com"]]
}

resource "google_artifact_registry_repository" "containers" {
  project       = var.artifact_registry_project_id
  location      = var.region
  repository_id = "td-sample-repo"
  format        = "DOCKER"
  description   = "Terradrift sample repository"
  labels        = local.sample_labels

  depends_on = [google_project_service.apis["artifactregistry.googleapis.com"]]
}

resource "google_secret_manager_secret" "app" {
  secret_id = "td-app-secret"
  labels    = local.sample_labels

  replication {
    auto {}
  }

  depends_on = [google_project_service.apis["secretmanager.googleapis.com"]]
}

resource "google_cloud_run_v2_service" "demo" {
  name                = "demo-serverless-service"
  location            = var.region
  ingress             = "INGRESS_TRAFFIC_ALL"
  deletion_protection = false

  labels = local.sample_labels

  template {
    service_account = google_service_account.app.email

    containers {
      image = "us-docker.pkg.dev/cloudrun/container/hello"
    }
  }

  depends_on = [google_project_service.apis["run.googleapis.com"]]
}

output "cloud_run_url" {
  description = "Public HTTPS URL of the Cloud Run service"
  value       = google_cloud_run_v2_service.demo.uri
}
