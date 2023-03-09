package provider

import (
	"context"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
)

func dataSourceOrasArtifact() *schema.Resource {
	return &schema.Resource{
		Description: "Reads a file from a remote OCI artifact.",

		ReadContext: dataSourceOrasArtifactRead,

		Schema: map[string]*schema.Schema{
			"name": {
				Description: "The reference of the remote artifact, including any tags or SHA256 repo digests.",
				Type:        schema.TypeString,
				Required:    true,
			},
			"output_path": {
				Description: "The output path of the artifact.",
				Type:        schema.TypeString,
				Required:    true,
			},
		},
	}
}

func dataSourceOrasArtifactRead(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	opts := meta.(*clients)

	reference := d.Get("name").(string)
	outputPath := d.Get("output_path").(string)

	repo, err := opts.NewRepository(reference)
	if err != nil {
		return diag.FromErr(err)
	}

	src, err := opts.CachedTarget(repo)
	if err != nil {
		return diag.FromErr(err)
	}

	dst, err := file.New(outputPath)
	if err != nil {
		return diag.FromErr(err)
	}

	desc, err := oras.Copy(ctx, src, repo.Reference.Reference, dst, repo.Reference.Reference, oras.DefaultCopyOptions)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(desc.Digest.String())

	return nil
}
