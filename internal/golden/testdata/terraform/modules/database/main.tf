# Same resource address as modules/network: a different module.
resource "aws_vpc" "main" {}

resource "aws_db_instance" "db" {
  vpc_id    = aws_vpc.main.id
  subnet_id = var.subnet_id
  other     = aws_subnet.private.id
}

variable "subnet_id" {
  type = string
}

output "endpoint" {
  value = aws_db_instance.db.endpoint
}
