resource "google_compute_network" "sample" {
  name                    = "td-sample-network"
  auto_create_subnetworks = false
  routing_mode            = "REGIONAL"
}

resource "google_compute_subnetwork" "sample" {
  name                     = "td-sample-subnet"
  region                   = "us-central1"
  ip_cidr_range            = "10.42.0.0/24"
  private_ip_google_access = true
  network                  = google_compute_network.sample.id
}

resource "google_compute_instance" "sample" {
  name         = "td-sample-vm"
  machine_type = "e2-micro"
  zone         = "us-central1-a"

  labels = {
    env = "dev"
  }

  tags = ["sample", "terradrift"]

  boot_disk {
    initialize_params {
      image = "projects/debian-cloud/global/images/family/debian-12"
      size  = 10
      type  = "pd-balanced"
    }
  }

  network_interface {
    subnetwork = google_compute_subnetwork.sample.id
  }
}

resource "google_pubsub_topic" "events" {
  name = "td-events-topic"

  labels = {
    env = "dev"
  }
}

resource "google_bigquery_dataset" "analytics" {
  dataset_id    = "td_analytics"
  location      = "US"
  friendly_name = "Terradrift Analytics"

  labels = {
    env = "dev"
  }
}

resource "google_artifact_registry_repository" "containers" {
  location      = "us-central1"
  repository_id = "td-sample-repo"
  format        = "DOCKER"
  description   = "Terradrift sample repository"

  labels = {
    env = "dev"
  }
}

resource "google_secret_manager_secret" "app" {
  secret_id = "td-app-secret"

  labels = {
    env = "dev"
  }

  replication {
    auto {}
  }
}
