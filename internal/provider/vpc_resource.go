package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/exodushost/terraform-provider-exodus/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &vpcResource{}
	_ resource.ResourceWithImportState = &vpcResource{}
)

func NewVPCResource() resource.Resource { return &vpcResource{} }

type vpcResource struct{ c *client.Client }

type vpcModel struct {
	ID     types.String `tfsdk:"id"`
	Name   types.String `tfsdk:"name"`
	Region types.String `tfsdk:"region"`
	CIDR   types.String `tfsdk:"cidr"`
}

func (r *vpcResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc"
}

func (r *vpcResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A private network between servers of one region. A server joins it with `vpc_id` " +
			"and gets a second network card (`eth1`); the address is configured by cloud-init on the next boot.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name shown in the panel.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region id, e.g. `ams`.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"cidr": schema.StringAttribute{
				MarkdownDescription: "Private IPv4 range from /16 to /28, e.g. `10.20.0.0/24` (the default).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *vpcResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req, resp)
}

func (r *vpcResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vpcModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var cidr *string // unset: the API picks its default
	if !plan.CIDR.IsUnknown() && !plan.CIDR.IsNull() {
		cidr = plan.CIDR.ValueStringPointer()
	}
	v, err := r.c.CreateVPC(ctx, plan.Name.ValueString(), plan.Region.ValueString(), cidr)
	if err != nil {
		resp.Diagnostics.AddError("Cannot create VPC", err.Error())
		return
	}
	plan.ID = types.StringValue(v.ID)
	if plan.CIDR.IsUnknown() {
		plan.CIDR = types.StringValue(v.CIDR)
	}
	// Saved before waiting, so a failed VPC is tracked (and tainted).
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if _, err := waitVPCReady(ctx, r.c, v.ID); err != nil {
		resp.Diagnostics.AddError("VPC did not become ready", err.Error())
	}
}

func (r *vpcResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vpcModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.c.GetVPC(ctx, state.ID.ValueString())
	if client.IsNotFound(err) || (err == nil && v.Status == "deleting") {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read VPC", err.Error())
		return
	}
	state.Name = types.StringValue(v.Name)
	state.Region = types.StringValue(v.RegionID)
	// The API stores the network address (10.20.0.5/24 → 10.20.0.0/24).
	if state.CIDR.IsNull() || state.CIDR.ValueString() != v.CIDR && truncCIDR(state.CIDR.ValueString()) != v.CIDR {
		state.CIDR = types.StringValue(v.CIDR)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: every configurable attribute forces replacement.
func (r *vpcResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("VPCs cannot be updated in place", "This is a bug in the provider.")
}

func (r *vpcResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vpcModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.c.DeleteVPC(ctx, state.ID.ValueString())
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot delete VPC", err.Error())
		return
	}
	if err := waitVPCGone(ctx, r.c, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("VPC was not deleted", err.Error())
	}
}

func (r *vpcResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
