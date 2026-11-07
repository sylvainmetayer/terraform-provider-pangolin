package resources

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// Removing api_key or headers from the config must clear them on the
// server: the key goes out as "" and the headers as null.
func TestBuildAIProviderRequest_ClearsRemovedKeyAndHeaders(t *testing.T) {
	plan := AIProviderResourceModel{
		Name: types.StringValue("p"), UpstreamURL: types.StringNull(), APIKey: types.StringNull(),
		AuthType: types.StringUnknown(), RoutingMode: types.StringUnknown(), Capabilities: types.SetUnknown(types.StringType),
		SkipTLSVerification: types.BoolUnknown(), Enabled: types.BoolValue(false),
	}
	req, err := buildAIProviderRequest(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(req)
	want := `{"name":"p","apiKey":"","headers":null,"enabled":false}`
	if string(b) != want {
		t.Errorf("body = %s, want %s", b, want)
	}
}

// The server answers an unset key as null or "", both read as null.
func TestAIProviderToModel_EmptyKeyIsNull(t *testing.T) {
	empty := ""
	m, err := aiProviderToModel(context.Background(), AIProviderResourceModel{}, &client.AIProvider{
		ProviderID: 3, Name: "p", Type: "custom", APIKey: &empty, Capabilities: []string{"openai_chat"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !m.APIKey.IsNull() || !m.UpstreamURL.IsNull() || m.Headers != nil || len(m.Capabilities.Elements()) != 1 {
		t.Errorf("model = %+v", m)
	}
}
