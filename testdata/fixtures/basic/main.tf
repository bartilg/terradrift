resource "google_storage_bucket" "logs" {
  name          = "td-logs-bucket"
  location      = "US"
  storage_class = "STANDARD"

  labels = {
    env = "dev"
  }

  versioning {
    enabled = true
  }
}

resource "google_service_account" "app" {
  account_id   = "app-sa"
  display_name = "App SA"
  description  = "Managed by Terraform"
}

resource "google_service_account" "computed" {
  account_id = var.sa_account_id
}
