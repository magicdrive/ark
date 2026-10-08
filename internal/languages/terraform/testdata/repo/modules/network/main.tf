resource "aws_vpc" "main" {
  cidr_block = var.cidr
}

resource "aws_subnet" "private" {
  vpc_id = aws_vpc.main.id
}

locals {
  subnet_id = aws_subnet.private.id
}
