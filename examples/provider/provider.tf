terraform {
  required_providers {
    pangolin = {
      source  = "stackopshq/pangolin"
      version = "1.7.0-sylvain.5" # fork pre-release, installed from a local mirror
    }
  }
}

provider "pangolin" {
  url     = "https://pangolin.example.com"
  api_key = var.pangolin_api_key
  org_id  = "my-org"
}
