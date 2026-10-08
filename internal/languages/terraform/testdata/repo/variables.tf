variable "region" {
  type    = string
  default = "us-east-1"
}

variable "instance_count" {
  type = number
}

variable "cidr" {
  type = string
}

variable "volume_names" {
  type = list(string)
}
