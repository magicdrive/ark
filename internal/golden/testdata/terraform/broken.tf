resource "aws_vpc" "broken" {
  cidr_block = var.cidr +
}

resource "aws_vpc" "after_broken" {
  cidr_block = var.cidr
}
