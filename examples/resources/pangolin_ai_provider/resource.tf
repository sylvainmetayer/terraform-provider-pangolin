# Hosted provider: the upstream URL and API key of the vendor.
resource "pangolin_ai_provider" "openai" {
  name         = "openai"
  type         = "openai"
  upstream_url = "https://api.openai.com/v1"
  api_key      = var.openai_api_key
}

# Self-hosted, OpenAI-compatible server (Ollama, vLLM...) without auth.
resource "pangolin_ai_provider" "ollama" {
  name         = "ollama"
  type         = "custom"
  upstream_url = "http://10.0.0.20:11434/v1"
  auth_type    = "none"
  capabilities = ["openai_chat", "v1_models"]
}

# Expose both through an inference resource.
resource "pangolin_resource" "llm" {
  name            = "llm-gateway"
  mode            = "inference"
  subdomain       = "llm"
  domain_id       = data.pangolin_domains.all.domains[0].domain_id
  ai_provider_ids = [pangolin_ai_provider.openai.id, pangolin_ai_provider.ollama.id]
}
