# Root module: two local child modules and one registry module.
module "network" {
  source = "./modules/network"
  cidr   = var.cidr
}

module "database" {
  source    = "./modules/database"
  subnet_id = module.network.subnet_id
}

module "vpc_remote" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.0.0"
}

resource "aws_instance" "web" {
  count         = var.instance_count
  ami           = data.aws_ami.ubuntu.id
  subnet_id     = module.network.subnet_id
  user_data     = templatefile("${path.module}/init.sh", { region = var.region })
  tags          = { Name = "web-${count.index}", Workspace = terraform.workspace }
  depends_on    = [aws_security_group.web]

  dynamic "ebs_block_device" {
    for_each = local.volumes
    content {
      device_name = ebs_block_device.value.name
    }
  }

  lifecycle {
    ignore_changes = [tags.Name]
  }
}

locals {
  volumes    = [for v in var.volume_names : { name = v }]
  web_ids    = aws_instance.web[*].id
  main       = "shadow"
}
