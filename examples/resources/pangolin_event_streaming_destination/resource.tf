# Push Pangolin's access and action logs to VictoriaLogs (JSON lines API).
resource "pangolin_event_streaming_destination" "victorialogs" {
  type = "http"
  config = jsonencode({
    name             = "victorialogs"
    url              = "https://logs.example.com/insert/jsonline?_msg_field=data.action&_time_field=timestamp&_stream_fields=event"
    authType         = "basic"
    basicCredentials = "pangolin:${var.victorialogs_password}"
    headers          = []
    format           = "ndjson"
    useBodyTemplate  = false
  })
  send_access_logs = true
  send_action_logs = true
}

# Archive every log type to an S3-compatible bucket.
resource "pangolin_event_streaming_destination" "archive" {
  type = "s3"
  config = jsonencode({
    name            = "archive"
    accessKeyId     = var.s3_access_key
    secretAccessKey = var.s3_secret_key
    region          = "us-east-1"
    bucket          = "pangolin-logs"
    endpoint        = "https://s3.example.com"
    format          = "ndjson"
    gzip            = true
  })
  send_request_logs    = true
  send_action_logs     = true
  send_access_logs     = true
  send_connection_logs = true
}
