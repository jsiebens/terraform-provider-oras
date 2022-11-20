provider "oras" {

  registry_auth {
    address     = "registry-1.docker.io"
    config_file = "~/.docker/config.json"
  }

  registry_auth {
    address             = "registry.my.company.com"
    config_file_content = var.plain_content_of_config_file
  }

  registry_auth {
    address  = "quay.io:8181"
    username = "someuser"
    password = "somepass"
  }

}
