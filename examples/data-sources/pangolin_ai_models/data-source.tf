data "pangolin_ai_models" "gpt" {
  provider_id = pangolin_ai_provider.openai.id
  query       = "gpt"
}

output "gpt_model_keys" {
  value = [for m in data.pangolin_ai_models.gpt.models : m.model_key]
}
