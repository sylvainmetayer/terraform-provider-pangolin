# Cap the organization at 500 USD a month.
resource "pangolin_ai_budget" "org" {
  amount = 500
  unit   = "usd"
}

# Cap one provider at 2 million tokens a day, report only.
resource "pangolin_ai_budget" "openai_daily" {
  provider_id = pangolin_ai_provider.openai.id
  amount      = 2000000
  unit        = "tokens"
  period      = "daily"
  enforcement = "soft"
}

# Cap one virtual API key.
resource "pangolin_ai_budget" "ci_key" {
  virtual_api_key_id = pangolin_virtual_api_key.ci.id
  amount             = 20
  unit               = "usd"
}
