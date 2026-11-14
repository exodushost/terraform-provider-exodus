package provider

import (
	"context"
	"net"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/exodushost/terraform-provider-exodus/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &firewallResource{}
	_ resource.ResourceWithImportState = &firewallResource{}
)

func NewFirewallResource() resource.Resource { return &firewallResource{} }

type firewallResource struct{ c *client.Client }

type firewallModel struct {
	ID    types.String `tfsdk:"id"`
	Name  types.String `tfsdk:"name"`
	Rules []ruleModel  `tfsdk:"rules"`
}

type ruleModel struct {
	Protocol    types.String `tfsdk:"protocol"`
	Ports       types.String `tfsdk:"ports"`
	Source      types.String `tfsdk:"source"`
	Description types.String `tfsdk:"description"`
}

var ruleType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"protocol":    types.StringType,
	"ports":       types.StringType,
	"source":      types.StringType,
	"description": types.StringType,
}}

func (r *firewallResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall"
}

func (r *firewallResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A set of inbound allow rules. Attach it to servers with their `firewall_ids`: " +
			"once a server has a firewall, only traffic that some rule allows gets in. Outbound traffic is not filtered.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name shown in the panel.",
				Required:            true,
			},
			"rules": schema.ListNestedAttribute{
				MarkdownDescription: "Inbound allow rules. An empty list blocks all inbound traffic to attached servers.",
				Optional:            true,
				Computed:            true,
				Default:             listdefault.StaticValue(types.ListValueMust(ruleType, []attr.Value{})),
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"protocol": schema.StringAttribute{
							MarkdownDescription: "`tcp`, `udp`, `icmp` or `any`.",
							Required:            true,
						},
						"ports": schema.StringAttribute{
							MarkdownDescription: "TCP/UDP only: `\"22\"`, `\"80,443\"` or a range `\"8000-8100\"`. Omit for all ports.",
							Optional:            true,
						},
						"source": schema.StringAttribute{
							MarkdownDescription: "IPv4 address or CIDR the traffic may come from. Omit for anywhere.",
							Optional:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "Note shown in the panel, up to 100 characters.",
							Optional:            true,
							Computed:            true,
							Default:             stringdefault.StaticString(""),
						},
					},
				},
			},
		},
	}
}

func (r *firewallResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req, resp)
}

func toAPIRules(rules []ruleModel) []client.Rule {
	out := make([]client.Rule, 0, len(rules))
	for _, m := range rules {
		out = append(out, client.Rule{
			Protocol:    m.Protocol.ValueString(),
			Ports:       m.Ports.ValueStringPointer(),
			Source:      m.Source.ValueStringPointer(),
			Description: m.Description.ValueString(),
		})
	}
	return out
}

func fromAPIRules(rules []client.Rule) []ruleModel {
	out := make([]ruleModel, 0, len(rules))
	for _, r := range rules {
		out = append(out, ruleModel{
			Protocol:    types.StringValue(r.Protocol),
			Ports:       types.StringPointerValue(r.Ports),
			Source:      types.StringPointerValue(r.Source),
			Description: types.StringValue(r.Description),
		})
	}
	return out
}

// keepRules returns the rules as written in config/state when the API's
// canonical form means the same, so "80-443" vs "80:443" is not a change.
func keepRules(written []ruleModel, api []client.Rule) []ruleModel {
	if written != nil && client.RulesEqual(toAPIRules(written), api) {
		return written
	}
	return fromAPIRules(api)
}

func (r *firewallResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan firewallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := r.c.CreateFirewall(ctx, plan.Name.ValueString(), toAPIRules(plan.Rules))
	if err != nil {
		resp.Diagnostics.AddError("Cannot create firewall", err.Error())
		return
	}
	plan.ID = types.StringValue(f.ID)
	plan.Rules = keepRules(plan.Rules, f.Rules)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *firewallResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state firewallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := r.c.GetFirewall(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read firewall", err.Error())
		return
	}
	state.Name = types.StringValue(f.Name)
	state.Rules = keepRules(state.Rules, f.Rules)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *firewallResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state firewallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := r.c.UpdateFirewall(ctx, state.ID.ValueString(), plan.Name.ValueString(), toAPIRules(plan.Rules))
	if err != nil {
		resp.Diagnostics.AddError("Cannot update firewall", err.Error())
		return
	}
	plan.ID = state.ID
	plan.Rules = keepRules(plan.Rules, f.Rules)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *firewallResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state firewallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.DeleteFirewall(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Cannot delete firewall", err.Error())
	}
}

func (r *firewallResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// truncCIDR is the network address of a CIDR ("10.20.0.5/24" → "10.20.0.0/24"),
// or the input when it doesn't parse.
func truncCIDR(s string) string {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		return s
	}
	return n.String()
}
