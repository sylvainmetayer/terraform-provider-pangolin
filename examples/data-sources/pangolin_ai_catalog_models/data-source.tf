# Model keys Pangolin suggests for Anthropic providers.
data "pangolin_ai_catalog_models" "anthropic" {
  type  = "anthropic"
  query = "sonnet"
}

output "anthropic_sonnet_keys" {
  value = data.pangolin_ai_catalog_models.anthropic.model_keys
}
