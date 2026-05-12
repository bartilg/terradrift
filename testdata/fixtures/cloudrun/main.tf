resource "google_cloud_run_v2_service" "demo" {
  name     = "demo-serverless-service"
  location = "us-central1"
  ingress  = "INGRESS_TRAFFIC_ALL"

  labels = {
    env = "dev"
  }

  template {
    service_account = "app-sa@demo-project.iam.gserviceaccount.com"

    containers {
      image = "us-docker.pkg.dev/cloudrun/container/hello"
    }
  }
}

resource "google_cloud_run_v2_service" "computed_location" {
  name     = "computed-service"
  location = var.region

  template {
    containers {
      image = "us-docker.pkg.dev/cloudrun/container/hello"
    }
  }
}

variable "region" {
  type    = string
  default = "us-central1"
}
