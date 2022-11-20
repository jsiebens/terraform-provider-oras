package provider

import (
	"context"
	"fmt"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/jsiebens/terraform-provider-oras/internal/cache"
	"github.com/mitchellh/go-homedir"
	"io"
	"net"
	"net/http"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"os"
	"strings"
	"time"
)

func init() {
	schema.DescriptionKind = schema.StringMarkdown
}

func New(version string) func() *schema.Provider {
	return func() *schema.Provider {
		p := &schema.Provider{
			Schema: map[string]*schema.Schema{
				"registry_auth": {
					Type:     schema.TypeSet,
					Optional: true,
					Elem: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"address": {
								Type:         schema.TypeString,
								Required:     true,
								ValidateFunc: validation.StringIsNotEmpty,
								Description:  "Address of the registry",
							},

							"username": {
								Type:        schema.TypeString,
								Optional:    true,
								Description: "Username for the registry.",
							},

							"password": {
								Type:        schema.TypeString,
								Optional:    true,
								Sensitive:   true,
								Description: "Password for the registry.",
							},

							"config_file": {
								Type:        schema.TypeString,
								Optional:    true,
								Default:     "~/.docker/config.json",
								Description: "Path to docker json file for registry auth. Defaults to `~/.docker/config.json`.",
							},

							"config_file_content": {
								Type:        schema.TypeString,
								Optional:    true,
								Description: "Plain content of the docker json file for registry auth.",
							},
						},
					},
				},
			},
			DataSourcesMap: map[string]*schema.Resource{
				"oras_artifact":      dataSourceOrasArtifact(),
				"oras_artifact_file": dataSourceOrasArtifactFile(),
			},
			ResourcesMap: map[string]*schema.Resource{},
		}

		p.ConfigureContextFunc = configure(version)

		return p
	}
}

type clients struct {
	version string
	client  *auth.Client
}

func (c *clients) NewRepository(reference string) (repo *remote.Repository, err error) {
	repo, err = remote.NewRepository(reference)
	if err != nil {
		return nil, err
	}
	repo.Client = c.client
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

func configure(version string) func(context.Context, *schema.ResourceData) (any, diag.Diagnostics) {
	return func(ctx context.Context, d *schema.ResourceData) (any, diag.Diagnostics) {

		creds := make(map[string]auth.Credential)

		if v, ok := d.GetOk("registry_auth"); ok {
			configureCreds, err := providerSetToCredentials(v.(*schema.Set))
			if err != nil {
				return nil, diag.Errorf("Error loading registry auth config: %s", err)
			}
			creds = configureCreds
		}

		client, err := authClient(version, creds)
		if err != nil {
			return nil, diag.Errorf("Error creating client: %s", err)
		}

		return &clients{version: version, client: client}, nil
	}
}

func authClient(version string, creds map[string]auth.Credential) (client *auth.Client, err error) {
	if err != nil {
		return nil, err
	}
	client = &auth.Client{
		Client: &http.Client{
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
	client.SetUserAgent("terraform-provider-oras/" + version)
	client.Credential = func(ctx context.Context, s string) (auth.Credential, error) {
		hostname := convertToHostname(s)
		if cred, ok := creds[hostname]; ok {
			return cred, nil
		}
		return auth.EmptyCredential, nil
	}
	return
}

func providerSetToCredentials(authList *schema.Set) (map[string]auth.Credential, error) {
	credentials := make(map[string]auth.Credential)

	for _, registryAuth := range authList.List() {
		cred := auth.Credential{}
		authMap := registryAuth.(map[string]interface{})
		hostname := convertToHostname(authMap["address"].(string))

		if username, ok := authMap["username"].(string); ok && username != "" {
			password := authMap["password"].(string)
			cred.Username = username
			cred.Password = password
		} else if configFileContent, ok := authMap["config_file_content"].(string); ok && configFileContent != "" {
			r := strings.NewReader(configFileContent)

			c, err := loadConfigFile(r)
			if err != nil {
				return nil, fmt.Errorf("error parsing docker registry config json: %v", err)
			}
			authFileConfig, err := c.GetAuthConfig(hostname)
			if err != nil {
				return nil, fmt.Errorf("couldn't find registry config for '%s' in file content", hostname)
			}
			cred.Username = authFileConfig.Username
			cred.Password = authFileConfig.Password
		} else if configFile, ok := authMap["config_file"].(string); ok && configFile != "" {
			filePath, err := homedir.Expand(configFile)
			if err != nil {
				return nil, err
			}

			r, err := os.Open(filePath)
			if err != nil {
				return nil, fmt.Errorf("could not open config file from filePath: %s. Error: %v", filePath, err)
			}
			c, err := loadConfigFile(r)
			if err != nil {
				return nil, fmt.Errorf("could not read and load config file: %v", err)
			}
			authFileConfig, err := c.GetAuthConfig(hostname)
			if err != nil {
				return nil, fmt.Errorf("could not get auth config (the credentialhelper did not work or was not found): %v", err)
			}
			cred.Username = authFileConfig.Username
			cred.Password = authFileConfig.Password
		}

		credentials[hostname] = cred
	}

	return credentials, nil
}

func loadConfigFile(configData io.Reader) (*configfile.ConfigFile, error) {
	configFile := configfile.New("")
	if err := configFile.LoadFromReader(configData); err != nil {
		if err := configFile.LegacyLoadFromReader(configData); err != nil {
			return nil, err
		}
	}
	return configFile, nil
}

func convertToHostname(url string) string {
	stripped := url
	// DevSkim: ignore DS137138
	if strings.HasPrefix(url, "http://") {
		// DevSkim: ignore DS137138
		stripped = strings.TrimPrefix(url, "http://")
	} else if strings.HasPrefix(url, "https://") {
		stripped = strings.TrimPrefix(url, "https://")
	}

	nameParts := strings.SplitN(stripped, "/", 2)

	return nameParts[0]
}
