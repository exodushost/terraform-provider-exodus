// Package provider implements the exodus Terraform provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/exodushost/terraform-provider-exodus/internal/client"
)

var _ provider.Provider = &exodusProvider{}

type exodusProvider struct {
	version string
}

type providerModel struct {
	Token    types.String `tfsdk:"token"`
	Endpoint types.String `tfsdk:"endpoint"`
}

// New returns the provider factory for providerserver.Serve and tests.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &exodusProvider{version: version}
	}
}

func (p *exodusProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "exodus"
	resp.Version = p.version
}

func (p *exodusProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages servers, SSH keys, firewalls and VPC networks on [exodus.host](https://exodus.host). " +
			"Authenticate with a project API token created in the panel (API tokens, «API-токены»); the provider works in that token's project.",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				MarkdownDescription: "API token (`exo_…`). Can also be set with the `EXODUS_TOKEN` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "API base URL. Defaults to `" + client.DefaultEndpoint + "`, or the `EXODUS_ENDPOINT` environment variable.",
				Optional:            true,
			},
		},
	}
}

func (p *exodusProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.Token.IsUnknown() || cfg.Endpoint.IsUnknown() {
		resp.Diagnostics.AddError("Unknown provider configuration",
			"token and endpoint must be known when the provider is configured.")
		return
	}

	token := os.Getenv("EXODUS_TOKEN")
	if !cfg.Token.IsNull() {
		token = cfg.Token.ValueString()
	}
	endpoint := os.Getenv("EXODUS_ENDPOINT")
	if !cfg.Endpoint.IsNull() {
		endpoint = cfg.Endpoint.ValueString()
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing API token",
			"Set token in the provider block or the EXODUS_TOKEN environment variable. "+
				"Create a token in the exodus.host panel: API tokens («API-токены»).")
		return
	}

	c := client.New(endpoint, token, "terraform-provider-exodus/"+p.version)
	if _, err := c.Init(ctx); err != nil {
		resp.Diagnostics.AddError("Cannot use the exodus.host API token", err.Error())
		return
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *exodusProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewSSHKeyResource,
		NewServerResource,
		NewFirewallResource,
		NewVPCResource,
	}
}

func (p *exodusProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}

// clientFrom is the shared Configure for resources.
func clientFrom(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", "expected *client.Client")
		return nil
	}
	return c
}
