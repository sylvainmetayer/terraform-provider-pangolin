package resources

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

// applyIDPReadResponse is the shared Read / ImportState mapping; these
// tests replay it against fixture wire payloads.

func TestIDP_ReadMapping_FollowsServerSecret(t *testing.T) {
	// GET /idp/{id} carries the variant on idpOidcConfig, not on idp.
	idp := &client.IDP{
		IDPId:         5,
		Name:          "google",
		Type:          "oidc",
		AutoProvision: true,
		Tags:          "prod",
	}
	cfg := &client.IDPOidcConfig{
		Variant:  "google",
		ClientID: "cid", ClientSecret: "rotated-in-ui",
		AuthURL: "https://a", TokenURL: "https://t",
		IdentifierPath: "sub", EmailPath: "email", NamePath: "name",
		Scopes: "openid email profile",
	}
	prior := IDPResourceModel{
		ID:           types.Int64Value(5),
		ClientSecret: types.StringValue("old-secret"),
		RedirectURL:  types.StringValue("https://cb"),
	}
	m := applyIDPReadResponse(prior, idp, cfg)

	if m.ClientSecret.ValueString() != "rotated-in-ui" {
		t.Errorf("ClientSecret must follow the server (drift detection), got %q", m.ClientSecret.ValueString())
	}
	if m.RedirectURL.ValueString() != "https://cb" {
		t.Errorf("RedirectURL must be preserved from prior, got %q", m.RedirectURL.ValueString())
	}
	if m.Name.ValueString() != "google" {
		t.Errorf("Name = %q", m.Name.ValueString())
	}
	if m.Variant.ValueString() != "google" {
		t.Errorf("Variant = %q", m.Variant.ValueString())
	}
	if m.ClientID.ValueString() != "cid" {
		t.Errorf("ClientID = %q", m.ClientID.ValueString())
	}
	if m.AuthURL.ValueString() != "https://a" || m.TokenURL.ValueString() != "https://t" {
		t.Errorf("URLs lost")
	}
	if m.IdentifierPath.ValueString() != "sub" ||
		m.EmailPath.ValueString() != "email" ||
		m.NamePath.ValueString() != "name" {
		t.Errorf("id paths lost")
	}
	if !m.AutoProvision.ValueBool() {
		t.Errorf("AutoProvision lost")
	}
	if m.Tags.ValueString() != "prod" {
		t.Errorf("Tags lost")
	}
	if m.Scopes.ValueString() != "openid email profile" {
		t.Errorf("Scopes lost")
	}
}

func TestIDP_ReadMapping_EmptySecretKeepsPrior(t *testing.T) {
	idp := &client.IDP{IDPId: 5, Name: "n", Type: "oidc"}
	cfg := &client.IDPOidcConfig{ClientID: "cid"}
	prior := IDPResourceModel{ClientSecret: types.StringValue("kept")}
	if got := applyIDPReadResponse(prior, idp, cfg).ClientSecret.ValueString(); got != "kept" {
		t.Errorf("ClientSecret = %q, want prior value when the server sends none", got)
	}
}

func TestIDP_ImportState_RecoversClientSecret(t *testing.T) {
	// ImportState seeds ID / ClientSecret / RedirectURL, then runs the
	// Read mapping. ClientSecret comes from the decrypted GET payload;
	// RedirectURL remains empty because GET does not return it.
	idp := &client.IDP{
		IDPId: 5, Name: "n",
		AutoProvision: false, Tags: "",
	}
	cfg := &client.IDPOidcConfig{
		ClientID: "cid", ClientSecret: "existing-secret", AuthURL: "a", TokenURL: "t",
		IdentifierPath: "sub", EmailPath: "email", NamePath: "name",
		Scopes: "openid",
	}
	state := applyIDPReadResponse(IDPResourceModel{
		ID:           types.Int64Value(int64(idp.IDPId)),
		ClientSecret: types.StringValue(""),
		RedirectURL:  types.StringValue(""),
	}, idp, cfg)
	// Assert every field on the imported state matches its source,
	// plus the secret recovery and non-recoverable redirect contracts.
	if got := state.ID.ValueInt64(); got != int64(idp.IDPId) {
		t.Errorf("ID = %d, want %d", got, idp.IDPId)
	}
	if got := state.Name.ValueString(); got != idp.Name {
		t.Errorf("Name = %q, want %q", got, idp.Name)
	}
	if got := state.AutoProvision.ValueBool(); got != idp.AutoProvision {
		t.Errorf("AutoProvision = %v, want %v", got, idp.AutoProvision)
	}
	if got := state.Tags.ValueString(); got != idp.Tags {
		t.Errorf("Tags = %q, want %q", got, idp.Tags)
	}
	if got := state.Variant.ValueString(); got != "oidc" {
		t.Errorf("Variant = %q, want normalized generic oidc", got)
	}
	if got := state.ClientID.ValueString(); got != cfg.ClientID {
		t.Errorf("ClientID = %q, want %q", got, cfg.ClientID)
	}
	if got := state.AuthURL.ValueString(); got != cfg.AuthURL {
		t.Errorf("AuthURL = %q, want %q", got, cfg.AuthURL)
	}
	if got := state.TokenURL.ValueString(); got != cfg.TokenURL {
		t.Errorf("TokenURL = %q, want %q", got, cfg.TokenURL)
	}
	if got := state.IdentifierPath.ValueString(); got != cfg.IdentifierPath {
		t.Errorf("IdentifierPath = %q, want %q", got, cfg.IdentifierPath)
	}
	if got := state.EmailPath.ValueString(); got != cfg.EmailPath {
		t.Errorf("EmailPath = %q, want %q", got, cfg.EmailPath)
	}
	if got := state.NamePath.ValueString(); got != cfg.NamePath {
		t.Errorf("NamePath = %q, want %q", got, cfg.NamePath)
	}
	if got := state.Scopes.ValueString(); got != cfg.Scopes {
		t.Errorf("Scopes = %q, want %q", got, cfg.Scopes)
	}
	if state.ClientSecret.ValueString() != cfg.ClientSecret {
		t.Errorf("ClientSecret = %q, want recovered API value", state.ClientSecret.ValueString())
	}
	if state.RedirectURL.ValueString() != "" {
		t.Errorf("RedirectURL must be empty after import (not returned by GET)")
	}
}

func TestResolveIDPVariant(t *testing.T) {
	tests := []struct {
		name   string
		idpVar string
		cfgVar string
		want   string
	}{
		{name: "GET shape, generic", cfgVar: "oidc", want: "oidc"},
		{name: "GET shape, google", cfgVar: "google", want: "google"},
		{name: "GET shape, azure", cfgVar: "azure", want: "azure"},
		{name: "LIST shape", idpVar: "google", want: "google"},
		{name: "config wins", idpVar: "oidc", cfgVar: "azure", want: "azure"},
		{name: "missing", want: "oidc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveIDPVariant(types.StringNull(), &client.IDP{Variant: tt.idpVar}, &client.IDPOidcConfig{Variant: tt.cfgVar})
			if got != tt.want {
				t.Fatalf("resolveIDPVariant(%q, %q) = %q, want %q", tt.idpVar, tt.cfgVar, got, tt.want)
			}
		})
	}
}

func TestResolveIDPVariant_KeepsKnownPrior(t *testing.T) {
	// A server that sends no variant must not flip an explicit
	// `variant = "google"` to "oidc" at apply time.
	got := resolveIDPVariant(types.StringValue("google"), &client.IDP{}, &client.IDPOidcConfig{})
	if got != "google" {
		t.Fatalf("variant = %q, want google (prior kept)", got)
	}
	// The server value still wins over the prior one.
	got = resolveIDPVariant(types.StringValue("google"), &client.IDP{}, &client.IDPOidcConfig{Variant: "azure"})
	if got != "azure" {
		t.Fatalf("variant = %q, want azure (server wins)", got)
	}
}

// -----------------------------------------------------------------------------
// IDP wire triple JSON round-trip - catches struct-tag drift
// -----------------------------------------------------------------------------

func TestIDP_TripleJSONRoundTrip(t *testing.T) {
	raw := `{
		"idp": {"idpId":5,"name":"google","type":"oidc","variant":"google","autoProvision":true,"tags":"prod"},
		"idpOidcConfig": {"clientId":"cid","clientSecret":"s","authUrl":"https://a","tokenUrl":"https://t","identifierPath":"sub","emailPath":"email","namePath":"name","scopes":"openid email profile"}
	}`
	// Reproduces the anonymous-struct shape used by client.GetIDP -
	// keeping the shape here (rather than importing it) tests that
	// the client's assumed JSON keys still round-trip correctly.
	var out struct {
		IDP           client.IDP           `json:"idp"`
		IDPOidcConfig client.IDPOidcConfig `json:"idpOidcConfig"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.IDP.IDPId != 5 || out.IDP.Name != "google" || out.IDP.Variant != "google" {
		t.Errorf("IDP block lost: %+v", out.IDP)
	}
	if out.IDPOidcConfig.ClientID != "cid" ||
		out.IDPOidcConfig.EmailPath != "email" ||
		out.IDPOidcConfig.Scopes != "openid email profile" {
		t.Errorf("OIDC block lost: %+v", out.IDPOidcConfig)
	}

	prior := IDPResourceModel{ClientSecret: types.StringValue("old")}
	m := applyIDPReadResponse(prior, &out.IDP, &out.IDPOidcConfig)
	if m.ClientSecret.ValueString() != "s" {
		t.Errorf("client_secret should follow the decrypted wire value, got %q", m.ClientSecret.ValueString())
	}
	if m.ClientID.ValueString() != "cid" {
		t.Errorf("client_id lost")
	}
}

// -----------------------------------------------------------------------------
// CreateIDPRequest / UpdateIDPRequest JSON shape - catches accidental drops.
// -----------------------------------------------------------------------------

func TestIDP_CreateRequest_FullShape(t *testing.T) {
	plan := IDPResourceModel{
		Name:           types.StringValue("n"),
		ClientID:       types.StringValue("cid"),
		ClientSecret:   types.StringValue("s"),
		AuthURL:        types.StringValue("https://a"),
		TokenURL:       types.StringValue("https://t"),
		IdentifierPath: types.StringValue("sub"),
		EmailPath:      types.StringValue("email"),
		NamePath:       types.StringValue("name"),
		Scopes:         types.StringValue("openid"),
		AutoProvision:  types.BoolValue(false),
		Tags:           types.StringValue(""),
		Variant:        types.StringValue("oidc"),
	}
	req := &client.CreateIDPRequest{
		Name:           plan.Name.ValueString(),
		ClientID:       plan.ClientID.ValueString(),
		ClientSecret:   plan.ClientSecret.ValueString(),
		AuthURL:        plan.AuthURL.ValueString(),
		TokenURL:       plan.TokenURL.ValueString(),
		IdentifierPath: plan.IdentifierPath.ValueString(),
		EmailPath:      plan.EmailPath.ValueString(),
		NamePath:       plan.NamePath.ValueString(),
		Scopes:         plan.Scopes.ValueString(),
		AutoProvision:  plan.AutoProvision.ValueBool(),
		Tags:           plan.Tags.ValueString(),
		Variant:        plan.Variant.ValueString(),
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// autoProvision=false, tags="" and variant="oidc" are all
	// omitempty on the CreateIDPRequest struct - the payload must
	// drop them entirely to stay compatible with pre-1.19 servers.
	want := `{"name":"n","clientId":"cid","clientSecret":"s","authUrl":"https://a","tokenUrl":"https://t","identifierPath":"sub","emailPath":"email","namePath":"name","scopes":"openid","variant":"oidc"}`
	if string(raw) != want {
		t.Errorf("payload = %s\nwant   = %s", raw, want)
	}
}

func TestIDPOidcConfig_UnmarshalNominal(t *testing.T) {
	// Direct sanity check: the OIDC config struct's JSON tags map
	// to lowerCamel per the client contract.
	raw := `{"variant":"azure","clientId":"cid","clientSecret":"s","authUrl":"a","tokenUrl":"t","identifierPath":"sub","emailPath":"email","namePath":"name","scopes":"openid"}`
	var c client.IDPOidcConfig
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.Variant != "azure" || c.ClientID != "cid" || c.ClientSecret != "s" || c.AuthURL != "a" {
		t.Errorf("OIDC config tag drift: %+v", c)
	}
	if c.IdentifierPath != "sub" || c.EmailPath != "email" ||
		c.NamePath != "name" || c.Scopes != "openid" {
		t.Errorf("OIDC config path tag drift: %+v", c)
	}
}
