package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func TestJSONObjectValidator(t *testing.T) {
	for in, wantErr := range map[string]bool{`{"url":"x"}`: false, `[1]`: true, `null`: true, `nope`: true} {
		resp := &validator.StringResponse{}
		jsonObjectValidator{}.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("config"), ConfigValue: types.StringValue(in)}, resp)
		if resp.Diagnostics.HasError() != wantErr {
			t.Errorf("%s: error = %v, want %v", in, resp.Diagnostics.HasError(), wantErr)
		}
	}
}

func TestEventStreamingToModel_KeepsConfigWhenNotDecrypted(t *testing.T) {
	m := eventStreamingToModel(types.StringValue(`{"a":1}`), &client.EventStreamingDestination{DestinationID: 1, Config: ""})
	if m.Config.ValueString() != `{"a":1}` || !m.LastError.IsNull() {
		t.Errorf("got config %s, last_error %s", m.Config, m.LastError)
	}
}
