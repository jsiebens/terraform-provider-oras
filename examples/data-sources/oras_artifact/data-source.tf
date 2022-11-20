data "oras_artifact" "example" {
  name        = "localhost:5000/hello-artifact:v2"
  output_path = "${path.module}/out/hello"
}