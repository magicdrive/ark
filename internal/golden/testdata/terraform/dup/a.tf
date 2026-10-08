resource "null_resource" "x" {}

output "x" {
  value = null_resource.x.id
}
