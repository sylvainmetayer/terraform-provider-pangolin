package client

import (
	"encoding/json"
	"strings"
	"testing"
)

// false and "" must reach the server, or auto_provision cannot be turned
// off and tags / email_path / name_path cannot be emptied.
func TestUpdateIDPRequestSendsClearedFields(t *testing.T) {
	b, _ := json.Marshal(&UpdateIDPRequest{})
	for _, k := range []string{`"autoProvision":false`, `"tags":""`, `"emailPath":""`, `"namePath":""`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("missing %s in %s", k, b)
		}
	}
}
