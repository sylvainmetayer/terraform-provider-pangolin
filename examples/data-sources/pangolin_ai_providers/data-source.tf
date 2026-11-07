# Names of every enabled OpenAI provider.
data "pangolin_ai_providers" "all" {}

output "openai_provider_names" {
  value = [for p in data.pangolin_ai_providers.all.providers : p.name if p.type == "openai" && p.enabled]
}
