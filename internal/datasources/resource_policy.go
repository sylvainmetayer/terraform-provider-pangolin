package datasources

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
	"github.com/stackopshq/terraform-provider-pangolin/internal/tfconv"
)

var _ datasource.DataSource = &ResourcePolicyDataSource{}

// ResourcePolicyDataSource exposes a single Pangolin resource policy
// by ID, with its grants, whitelist and rules. Secrets are reported
// as booleans only.
type ResourcePolicyDataSource struct {
	client *client.Client
}

// ResourcePolicyRuleDataModel is one rule of the policy.
type ResourcePolicyRuleDataModel struct {
	RuleID   types.Int64  `tfsdk:"rule_id"`
	Action   types.String `tfsdk:"action"`
	Match    types.String `tfsdk:"match"`
	Value    types.String `tfsdk:"value"`
	Priority types.Int64  `tfsdk:"priority"`
	Enabled  types.Bool   `tfsdk:"enabled"`
}

// ResourcePolicyDataSourceModel describes the data source data model.
type ResourcePolicyDataSourceModel struct {
	ID                              types.Int64                   `tfsdk:"id"`
	NiceID                          types.String                  `tfsdk:"nice_id"`
	Name                            types.String                  `tfsdk:"name"`
	SSO                             types.Bool                    `tfsdk:"sso"`
	SkipToIdpID                     types.Int64                   `tfsdk:"skip_to_idp_id"`
	RoleIDs                         []types.Int64                 `tfsdk:"role_ids"`
	UserIDs                         []types.String                `tfsdk:"user_ids"`
	EmailWhitelistEnabled           types.Bool                    `tfsdk:"email_whitelist_enabled"`
	WhitelistEmails                 []types.String                `tfsdk:"whitelist_emails"`
	ApplyRules                      types.Bool                    `tfsdk:"apply_rules"`
	Rules                           []ResourcePolicyRuleDataModel `tfsdk:"rules"`
	HasPassword                     types.Bool                    `tfsdk:"has_password"`
	HasPincode                      types.Bool                    `tfsdk:"has_pincode"`
	HasHeaderAuth                   types.Bool                    `tfsdk:"has_header_auth"`
	HeaderAuthExtendedCompatibility types.Bool                    `tfsdk:"header_auth_extended_compatibility"`
}

// NewResourcePolicyDataSource returns a new data source factory.
func NewResourcePolicyDataSource() datasource.DataSource {
	return &ResourcePolicyDataSource{}
}

func (d *ResourcePolicyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_policy"
}

func (d *ResourcePolicyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Looks up a single shared Pangolin resource policy (Enterprise) by ID; the inline default " +
			"policy of a resource is refused. Secrets are never returned: " +
			"`has_password`, `has_pincode` and `has_header_auth` only tell whether one is set. " +
			"To browse the shared policies of the org, use `pangolin_resource_policies`.",
		Attributes: map[string]schema.Attribute{
			"id":                      schema.Int64Attribute{Description: "The resource policy ID to look up.", Required: true},
			"nice_id":                 schema.StringAttribute{Description: "Readable ID, unique per org.", Computed: true},
			"name":                    schema.StringAttribute{Description: "Display name of the policy.", Computed: true},
			"sso":                     schema.BoolAttribute{Description: "Whether Pangolin SSO authentication is required.", Computed: true},
			"skip_to_idp_id":          schema.Int64Attribute{Description: "IdP the login page jumps straight to, or `null`.", Computed: true},
			"role_ids":                schema.ListAttribute{Description: "IDs of the roles granted access (the org admin role is not listed).", Computed: true, ElementType: types.Int64Type},
			"user_ids":                schema.ListAttribute{Description: "IDs of the users granted access.", Computed: true, ElementType: types.StringType},
			"email_whitelist_enabled": schema.BoolAttribute{Description: "Whether the email whitelist is enabled.", Computed: true},
			"whitelist_emails":        schema.ListAttribute{Description: "Emails allowed through the whitelist.", Computed: true, ElementType: types.StringType},
			"apply_rules":             schema.BoolAttribute{Description: "Whether the access rules are evaluated.", Computed: true},
			"rules": schema.ListNestedAttribute{
				Description: "Access rules of the policy.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"rule_id":  schema.Int64Attribute{Description: "The rule ID.", Computed: true},
						"action":   schema.StringAttribute{Description: "The rule action (`ACCEPT`, `DROP`, `PASS`).", Computed: true},
						"match":    schema.StringAttribute{Description: "The match type.", Computed: true},
						"value":    schema.StringAttribute{Description: "The value matched against.", Computed: true},
						"priority": schema.Int64Attribute{Description: "The rule priority (lower number = higher priority).", Computed: true},
						"enabled":  schema.BoolAttribute{Description: "Whether the rule is enabled.", Computed: true},
					},
				},
			},
			"has_password":                       schema.BoolAttribute{Description: "Whether a password is set.", Computed: true},
			"has_pincode":                        schema.BoolAttribute{Description: "Whether a PIN code is set.", Computed: true},
			"has_header_auth":                    schema.BoolAttribute{Description: "Whether header authentication is set.", Computed: true},
			"header_auth_extended_compatibility": schema.BoolAttribute{Description: "Extended compatibility flag of the header authentication, or `null` when none is set.", Computed: true},
		},
	}
}

func (d *ResourcePolicyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *client.Client")
		return
	}
	d.client = c
}

func (d *ResourcePolicyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg ResourcePolicyDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy, err := d.client.GetResourcePolicy(ctx, int(cfg.ID.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Failed to read resource policy", err.Error())
		return
	}
	shared, err := d.client.IsSharedResourcePolicy(ctx, int(cfg.ID.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Failed to read resource policy", err.Error())
		return
	}
	if !shared {
		resp.Diagnostics.AddError("Not a shared resource policy",
			fmt.Sprintf("Resource policy %d is the inline default policy of a resource, not a shared policy.", cfg.ID.ValueInt64()))
		return
	}

	state := resourcePolicyDataModel(policy)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// resourcePolicyDataModel maps a GET payload to the data source model.
func resourcePolicyDataModel(p *client.ResourcePolicy) ResourcePolicyDataSourceModel {
	m := ResourcePolicyDataSourceModel{
		ID:                              types.Int64Value(int64(p.ResourcePolicyID)),
		NiceID:                          types.StringValue(p.NiceID),
		Name:                            types.StringValue(p.Name),
		SSO:                             types.BoolValue(p.SSO),
		SkipToIdpID:                     tfconv.Int64FromIntPtr(p.IDPID),
		RoleIDs:                         []types.Int64{},
		UserIDs:                         []types.String{},
		EmailWhitelistEnabled:           types.BoolValue(p.EmailWhitelistEnabled),
		WhitelistEmails:                 []types.String{},
		ApplyRules:                      types.BoolValue(p.ApplyRules),
		Rules:                           []ResourcePolicyRuleDataModel{},
		HasPassword:                     types.BoolValue(p.PasswordID != nil),
		HasPincode:                      types.BoolValue(p.PincodeID != nil),
		HasHeaderAuth:                   types.BoolValue(p.HasHeaderAuth()),
		HeaderAuthExtendedCompatibility: types.BoolNull(),
	}
	if p.HasHeaderAuth() {
		m.HeaderAuthExtendedCompatibility = tfconv.BoolFromPtr(p.HeaderAuth.ExtendedCompatibility)
	}
	// The server returns these lists unordered: sort them so the data
	// source output is stable between refreshes.
	roles := append([]client.ResourcePolicyRole(nil), p.Roles...)
	sort.Slice(roles, func(i, j int) bool { return roles[i].RoleID < roles[j].RoleID })
	users := append([]client.ResourcePolicyUser(nil), p.Users...)
	sort.Slice(users, func(i, j int) bool { return users[i].UserID < users[j].UserID })
	emails := append([]client.ResourcePolicyWhitelistEntry(nil), p.EmailWhitelist...)
	sort.Slice(emails, func(i, j int) bool { return emails[i].Email < emails[j].Email })
	rules := append([]client.ResourcePolicyRule(nil), p.Rules...)
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority < rules[j].Priority
		}
		return rules[i].RuleID < rules[j].RuleID
	})
	for _, role := range roles {
		m.RoleIDs = append(m.RoleIDs, types.Int64Value(int64(role.RoleID)))
	}
	for _, user := range users {
		m.UserIDs = append(m.UserIDs, types.StringValue(user.UserID))
	}
	for _, entry := range emails {
		m.WhitelistEmails = append(m.WhitelistEmails, types.StringValue(entry.Email))
	}
	for _, rule := range rules {
		m.Rules = append(m.Rules, ResourcePolicyRuleDataModel{
			RuleID:   types.Int64Value(int64(rule.RuleID)),
			Action:   types.StringValue(rule.Action),
			Match:    types.StringValue(rule.Match),
			Value:    types.StringValue(rule.Value),
			Priority: types.Int64Value(int64(rule.Priority)),
			Enabled:  types.BoolValue(rule.IsEnabled()),
		})
	}
	return m
}
