data "oras_artifact_file" "example" {
  name     = "localhost:5000/hello-artifact:v2"
  filename = "artifact.txt"
}