output "network_id" {
  value = module.network.vpc_id
}

output "missing" {
  value = module.network.no_such_output
}

output "remote_vpc" {
  value = module.vpc_remote.vpc_id
}

output "endpoint" {
  value = module.database.endpoint
}

output "undeclared" {
  value = aws_vpc.main.id
}
