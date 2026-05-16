variable "project_id" {
  description = "GCP project ID or numeric project number"
  type        = string
}

variable "artifact_registry_project_id" {
  description = "GCP project ID for Artifact Registry; this API does not accept numeric project numbers"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.artifact_registry_project_id))
    error_message = "artifact_registry_project_id must be a GCP project ID such as my-project-123, not a numeric project number."
  }
}

variable "region" {
  description = "Region to deploy resources into"
  type        = string
  default     = "us-central1"
}

variable "zone" {
  description = "Zone to deploy resources into"
  type        = string
  default     = "us-central1-a"
}
