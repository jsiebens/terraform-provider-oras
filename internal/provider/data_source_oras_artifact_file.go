package provider

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func dataSourceOrasArtifactFile() *schema.Resource {
	return &schema.Resource{
		Description: "Reads a file from a remote OCI artifact.",

		ReadContext: dataSourceOrasArtifactFileRead,

		Schema: map[string]*schema.Schema{
			"name": {
				Description: "The reference of the remote artifact, including any tags or SHA256 repo digests.",
				Type:        schema.TypeString,
				Required:    true,
			},
			"filename": {
				Type:     schema.TypeString,
				Required: true,
			},
			"content": {
				Description: "Raw content of the file that was read, as UTF-8 encoded string.",
				Type:        schema.TypeString,
				Computed:    true,
			},
			"content_base64": {
				Description: "Base64 encoded version of the file content (use this when dealing with binary data).",
				Type:        schema.TypeString,
				Computed:    true,
			},
		},
	}
}

func dataSourceOrasArtifactFileRead(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	opts := meta.(*clients)

	reference := d.Get("name").(string)
	filename := d.Get("filename").(string)

	repo, err := opts.NewRepository(reference)
	if err != nil {
		return diag.FromErr(err)
	}

	src, err := opts.CachedTarget(repo)
	if err != nil {
		return diag.FromErr(err)
	}

	temp, err := os.MkdirTemp("", "terraform-oras-provider-")
	if err != nil {
		return diag.FromErr(err)
	}
	defer os.RemoveAll(temp)

	dst, err := file.New(temp)
	if err != nil {
		return diag.FromErr(err)
	}

	if _, err := oras.Copy(ctx, src, repo.Reference.Reference, dst, repo.Reference.Reference, oras.DefaultCopyOptions); err != nil {
		return diag.FromErr(err)
	}

	content, err := os.ReadFile(filepath.Join(temp, filename))
	if err != nil {
		return diag.FromErr(err)
	}

	// Set the content both as UTF-8 string, and as base64 encoded string
	_ = d.Set("content", string(content))
	_ = d.Set("content_base64", base64.StdEncoding.EncodeToString(content))

	// Use the hexadecimal encoding of the checksum of the file content as ID
	checksum := sha1.Sum(content)
	d.SetId(hex.EncodeToString(checksum[:]))

	return nil
}
