package provider

import (
	"context"
	"github.com/jsiebens/terraform-provider-oras/internal/cache"
	credential "github.com/jsiebens/terraform-provider-oras/internal/credentials"
	"net"
	"net/http"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func init() {
	schema.DescriptionKind = schema.StringMarkdown
}

func New(version string) func() *schema.Provider {
	return func() *schema.Provider {
		p := &schema.Provider{
			DataSourcesMap: map[string]*schema.Resource{
				"oras_artifact_file": dataSourceOrasArtifactFile(),
			},
			ResourcesMap: map[string]*schema.Resource{},
		}

		p.ConfigureContextFunc = configure(version, p)

		return p
	}
}

type clients struct {
	version string
}

func (c *clients) NewRepository(reference string) (repo *remote.Repository, err error) {
	repo, err = remote.NewRepository(reference)
	if err != nil {
		return nil, err
	}
	hostname := repo.Reference.Registry
	repo.PlainHTTP = c.isPlainHttp(hostname)
	if repo.Client, err = c.authClient(hostname); err != nil {
		return nil, err
	}
	return
}

func (c *clients) CachedTarget(src oras.ReadOnlyTarget) (oras.ReadOnlyTarget, error) {
	root := os.Getenv("ORAS_CACHE")
	if root != "" {
		ociStore, err := oci.New(root)
		if err != nil {
			return nil, err
		}
		return cache.New(src, ociStore), nil
	}
	return src, nil
}

func (c *clients) isPlainHttp(registry string) bool {
	host, _, _ := net.SplitHostPort(registry)
	if host == "localhost" || registry == "localhost" {
		return true
	}
	return false
}

func (c *clients) authClient(registry string) (client *auth.Client, err error) {
	if err != nil {
		return nil, err
	}
	client = &auth.Client{
		Client: &http.Client{
			// default value are derived from http.DefaultTransport
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
		Cache: auth.NewCache(),
	}
	client.SetUserAgent("terraform-provider-oras/" + c.version)

	store, err := credential.NewStore()
	if err != nil {
		return nil, err
	}
	// For a user case with a registry from 'docker.io', the hostname is "registry-1.docker.io"
	// According to the the behavior of Docker CLI,
	// credential under key "https://index.docker.io/v1/" should be provided
	if registry == "docker.io" {
		client.Credential = func(ctx context.Context, hostname string) (auth.Credential, error) {
			if hostname == "registry-1.docker.io" {
				hostname = "https://index.docker.io/v1/"
			}
			return store.Credential(ctx, hostname)
		}
	} else {
		client.Credential = store.Credential
	}

	return
}

func configure(version string, p *schema.Provider) func(context.Context, *schema.ResourceData) (any, diag.Diagnostics) {
	return func(context.Context, *schema.ResourceData) (any, diag.Diagnostics) {
		return &clients{version: version}, nil
	}
}
