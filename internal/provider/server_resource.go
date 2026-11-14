package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/exodushost/terraform-provider-exodus/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = &serverResource{}
	_ resource.ResourceWithImportState = &serverResource{}
	_ resource.ResourceWithModifyPlan  = &serverResource{}
)

func NewServerResource() resource.Resource { return &serverResource{} }

type serverResource struct{ c *client.Client }

type serverModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Region       types.String `tfsdk:"region"`
	Image        types.String `tfsdk:"image"`
	VCPU         types.Int64  `tfsdk:"vcpu"`
	RAMGB        types.Int64  `tfsdk:"ram_gb"`
	DiskGB       types.Int64  `tfsdk:"disk_gb"`
	SSHKeyIDs    types.Set    `tfsdk:"ssh_key_ids"`
	FirewallIDs  types.Set    `tfsdk:"firewall_ids"`
	VPCID        types.String `tfsdk:"vpc_id"`
	Status       types.String `tfsdk:"status"`
	PrivateIP    types.String `tfsdk:"private_ip"`
	PublicIP     types.String `tfsdk:"public_ip"`
	SSHPort      types.Int64  `tfsdk:"ssh_port"`
	VPCIP        types.String `tfsdk:"vpc_ip"`
	RootPassword types.String `tfsdk:"root_password"`
}

func (r *serverResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (r *serverResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A virtual server. It is billed by the hour while it exists; a stopped server is billed for its disk only. " +
			"SSH is reachable at `public_ip:ssh_port`. Sizes and images: `GET https://exodus.host/api/v1/catalog`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"name": schema.StringAttribute{
				MarkdownDescription: "Hostname: lowercase letters, digits and hyphens. Changing it replaces the server.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region id, e.g. `ams`. Changing it replaces the server.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"image": schema.StringAttribute{
				MarkdownDescription: "Image id, e.g. `ubuntu-24.04`, `debian-12`. Changing it replaces the server.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"vcpu": schema.Int64Attribute{
				MarkdownDescription: "Virtual CPUs. Changing it stops the server for the resize and starts it again.",
				Required:            true,
			},
			"ram_gb": schema.Int64Attribute{
				MarkdownDescription: "Memory in GB. Changing it stops the server for the resize and starts it again.",
				Required:            true,
			},
			"disk_gb": schema.Int64Attribute{
				MarkdownDescription: "Boot disk in GB. It can only grow, without a restart; the OS extends the partition on the next boot.",
				Required:            true,
			},
			"ssh_key_ids": schema.SetAttribute{
				MarkdownDescription: "`exodus_ssh_key` ids added to root's `authorized_keys` at creation. Changing them replaces the server.",
				ElementType:         types.StringType,
				Optional:            true,
				PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplaceIf(
					// An imported server has no known keys: adopting it must not destroy it.
					func(_ context.Context, req planmodifier.SetRequest, resp *setplanmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = !req.StateValue.IsNull()
					},
					"Changing SSH keys replaces the server.", "Changing SSH keys replaces the server.",
				)},
			},
			"firewall_ids": schema.SetAttribute{
				MarkdownDescription: "`exodus_firewall` ids applied to the server. Changed in place.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"vpc_id": schema.StringAttribute{
				MarkdownDescription: "`exodus_vpc` to join (same region). Changed in place; the `eth1` address is configured on the next boot.",
				Optional:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "`running`, `stopped`, `error`, or an operation in progress.",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"private_ip": schema.StringAttribute{
				MarkdownDescription: "Address on the node's internal network (`eth0`).",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"public_ip": schema.StringAttribute{
				MarkdownDescription: "Public address of the node; SSH listens on it at `ssh_port`.",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"ssh_port": schema.Int64Attribute{
				MarkdownDescription: "Port forwarded to the server's port 22: `ssh -p <ssh_port> root@<public_ip>`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"vpc_ip": schema.StringAttribute{
				MarkdownDescription: "Address in the VPC, if the server is in one.",
				Computed:            true,
			},
			"root_password": schema.StringAttribute{
				MarkdownDescription: "Root password generated at creation. Not available for imported servers; changes made later in the panel are not tracked.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       keep,
			},
		},
	}
}

func (r *serverResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req, resp)
}

func (r *serverResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if known(plan.DiskGB) && plan.DiskGB.ValueInt64() < state.DiskGB.ValueInt64() {
		resp.Diagnostics.AddAttributeError(path.Root("disk_gb"), "The disk can only grow",
			fmt.Sprintf("disk_gb is %d and can't go down to %d. To start over with a smaller disk, replace the server: terraform apply -replace=<address>.",
				state.DiskGB.ValueInt64(), plan.DiskGB.ValueInt64()))
	}
	if (known(plan.VCPU) && plan.VCPU != state.VCPU) || (known(plan.RAMGB) && plan.RAMGB != state.RAMGB) {
		resp.Diagnostics.AddAttributeWarning(path.Root("vcpu"), "The server will restart",
			"Changing vcpu or ram_gb shuts the server down for the resize and starts it again if it was running.")
	}
}

func known(v types.Int64) bool { return !v.IsNull() && !v.IsUnknown() }

func stringsOf(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	out := []string{}
	if s.IsNull() || s.IsUnknown() {
		return out
	}
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	return out
}

// fill copies what the API reports into the model. Configured attributes are
// left as planned unless fromAPI (Read), so a refresh shows real drift.
func fill(ctx context.Context, m *serverModel, s *client.Server, fromAPI bool, diags *diag.Diagnostics) {
	m.ID = types.StringValue(s.ID)
	m.Status = types.StringValue(s.Status)
	m.PrivateIP = types.StringValue(s.PrivateIP)
	m.PublicIP = types.StringPointerValue(s.PublicIP)
	m.SSHPort = types.Int64PointerValue(s.SSHPort)
	m.VPCIP = types.StringPointerValue(s.VPCIP)
	if !fromAPI {
		return
	}
	m.Name = types.StringValue(s.Name)
	m.Region = types.StringValue(s.RegionID)
	m.Image = types.StringValue(s.ImageID)
	m.VCPU = types.Int64Value(s.VCPU)
	m.RAMGB = types.Int64Value(s.RAMMB / 1024)
	m.DiskGB = types.Int64Value(s.DiskGB)
	m.VPCID = types.StringPointerValue(s.VPCID)
	// No firewalls and none configured: keep null rather than an empty set.
	if len(s.FirewallIDs) > 0 || !m.FirewallIDs.IsNull() {
		ids, d := types.SetValueFrom(ctx, types.StringType, s.FirewallIDs)
		diags.Append(d...)
		m.FirewallIDs = ids
	}
}

func (r *serverResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	create := client.CreateServerRequest{
		Name:        plan.Name.ValueString(),
		RegionID:    plan.Region.ValueString(),
		ImageID:     plan.Image.ValueString(),
		VCPU:        plan.VCPU.ValueInt64(),
		RAMGB:       plan.RAMGB.ValueInt64(),
		DiskGB:      plan.DiskGB.ValueInt64(),
		SSHKeyIDs:   stringsOf(ctx, plan.SSHKeyIDs, &resp.Diagnostics),
		FirewallIDs: stringsOf(ctx, plan.FirewallIDs, &resp.Diagnostics),
		VPCID:       plan.VPCID.ValueStringPointer(),
	}
	if resp.Diagnostics.HasError() {
		return
	}
	s, password, err := r.c.CreateServer(ctx, create)
	if err != nil {
		resp.Diagnostics.AddError("Cannot create server", err.Error())
		return
	}
	plan.RootPassword = types.StringValue(password)
	fill(ctx, &plan, s, false, &resp.Diagnostics)
	// Saved before waiting, so a server that fails to come up is tracked (and tainted).
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)

	s, err = waitServerIdle(ctx, r.c, s.ID)
	if err != nil {
		resp.Diagnostics.AddError("Server did not come up", err.Error())
		return
	}
	fill(ctx, &plan, s, false, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serverResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.c.GetServer(ctx, state.ID.ValueString())
	if client.IsNotFound(err) || (err == nil && (s.Status == "deleted" || s.Status == "deleting")) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read server", err.Error())
		return
	}
	fill(ctx, &state, s, true, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *serverResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	fail := func(what string, err error) { resp.Diagnostics.AddError(what, err.Error()) }

	if !plan.FirewallIDs.Equal(state.FirewallIDs) {
		ids := stringsOf(ctx, plan.FirewallIDs, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		if err := r.c.SetServerFirewalls(ctx, id, ids); err != nil {
			fail("Cannot change the server's firewalls", err)
			return
		}
	}

	if !plan.VPCID.Equal(state.VPCID) {
		if !state.VPCID.IsNull() {
			if err := r.vpcStep(ctx, id, nil); err != nil {
				fail("Cannot leave the VPC", err)
				return
			}
		}
		if !plan.VPCID.IsNull() {
			if err := r.vpcStep(ctx, id, plan.VPCID.ValueStringPointer()); err != nil {
				fail("Cannot join the VPC", err)
				return
			}
		}
	}

	if !plan.VCPU.Equal(state.VCPU) || !plan.RAMGB.Equal(state.RAMGB) || !plan.DiskGB.Equal(state.DiskGB) {
		if err := r.resize(ctx, id, plan.VCPU.ValueInt64(), plan.RAMGB.ValueInt64(), plan.DiskGB.ValueInt64()); err != nil {
			fail("Cannot resize the server", err)
			return
		}
	}

	s, err := waitServerIdle(ctx, r.c, id)
	if err != nil {
		fail("Server did not settle", err)
		return
	}
	plan.ID = state.ID
	fill(ctx, &plan, s, false, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// vpcStep joins vpcID (or leaves with nil) and waits for it to finish.
func (r *serverResource) vpcStep(ctx context.Context, id string, vpcID *string) error {
	if _, err := waitServerIdle(ctx, r.c, id); err != nil {
		return err
	}
	if err := r.c.SetServerVPC(ctx, id, vpcID); err != nil {
		return err
	}
	_, err := waitServerIdle(ctx, r.c, id)
	return err
}

// resize changes the size; CPU/RAM need the server stopped, so a running
// server is shut down first and started again afterwards.
func (r *serverResource) resize(ctx context.Context, id string, vcpu, ramGB, diskGB int64) error {
	s, err := waitServerIdle(ctx, r.c, id)
	if err != nil {
		return err
	}
	cpuRAM := s.VCPU != vcpu || s.RAMMB != ramGB*1024
	if !cpuRAM && s.DiskGB == diskGB {
		return nil
	}
	restart := cpuRAM && s.Status == "running"
	if restart {
		if err := r.c.ServerAction(ctx, id, "shutdown"); err != nil {
			return fmt.Errorf("shut down before resize: %w", err)
		}
		if _, err := waitServerIdle(ctx, r.c, id); err != nil {
			return err
		}
	}
	if err := r.c.ResizeServer(ctx, id, vcpu, ramGB, diskGB); err != nil {
		return err
	}
	if _, err := waitServerIdle(ctx, r.c, id); err != nil {
		return err
	}
	if restart {
		if err := r.c.ServerAction(ctx, id, "start"); err != nil {
			return fmt.Errorf("start after resize: %w", err)
		}
	}
	return nil
}

func (r *serverResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	// While another operation runs the API answers 409; retry until it ends.
	err := poll(ctx, "server "+id+" to accept deletion", func() (bool, error) {
		err := r.c.DeleteServer(ctx, id)
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
			return false, nil
		}
		return true, err
	})
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot delete server", err.Error())
		return
	}
	if err := waitServerGone(ctx, r.c, id); err != nil {
		resp.Diagnostics.AddError("Server was not deleted", err.Error())
	}
}

func (r *serverResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
