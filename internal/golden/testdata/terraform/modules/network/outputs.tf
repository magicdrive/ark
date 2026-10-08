output "vpc_id" {
  value = aws_vpc.main.id
}

output "subnet_id" {
  value = local.subnet_id
}
