data "aws_ami" "ubuntu" {
  most_recent = true
}

resource "aws_security_group" "web" {
  name = "web-${var.region}"
}
