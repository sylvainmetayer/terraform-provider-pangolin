resource "pangolin_ai_model" "gpt4o" {
  provider_id = pangolin_ai_provider.openai.id
  model_key   = "gpt-4o"
  name        = "GPT-4o"
}

# Refuse a model the provider would otherwise serve.
resource "pangolin_ai_model" "no_o1" {
  provider_id = pangolin_ai_provider.openai.id
  model_key   = "o1"
  name        = "o1"
  list_type   = "block"
}
