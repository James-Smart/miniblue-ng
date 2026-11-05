# Generic Linux VM fixture for miniblue.
#
# Creates a resource group, VNet/subnet, public IP, network security group,
# network interface, SSH public key, Linux VM, managed data disk and a VM
# extension. It uses only public example values and no private configuration.
#
# Run against miniblue:
#   bash scripts/trust-cert.sh
#   cd examples/terraform/vm
#   terraform init
#   terraform apply -auto-approve
#   terraform plan -detailed-exitcode   # expect exit code 0 (no changes)
#   terraform destroy -auto-approve

resource "azurerm_resource_group" "example" {
  name     = "miniblue-vm-example"
  location = "eastus"

  tags = {
    environment = "example"
  }
}

resource "azurerm_virtual_network" "example" {
  name                = "miniblue-vm-vnet"
  address_space       = ["10.0.0.0/16"]
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name
}

resource "azurerm_subnet" "example" {
  name                 = "miniblue-vm-subnet"
  resource_group_name  = azurerm_resource_group.example.name
  virtual_network_name = azurerm_virtual_network.example.name
  address_prefixes     = ["10.0.1.0/24"]
}

resource "azurerm_public_ip" "example" {
  name                = "miniblue-vm-pip"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name
  allocation_method   = "Dynamic"
}

resource "azurerm_network_security_group" "example" {
  name                = "miniblue-vm-nsg"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name

  security_rule {
    name                       = "ssh"
    priority                   = 1001
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "22"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }
}

resource "azurerm_network_interface" "example" {
  name                = "miniblue-vm-nic"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name

  ip_configuration {
    name                          = "ipconfig1"
    subnet_id                     = azurerm_subnet.example.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.example.id
  }
}

resource "azurerm_network_interface_security_group_association" "example" {
  network_interface_id      = azurerm_network_interface.example.id
  network_security_group_id = azurerm_network_security_group.example.id
}

# A throwaway public key generated for this example. Replace it with your own
# if you want to connect from outside the emulator; miniblue never installs it.
resource "azurerm_ssh_public_key" "example" {
  name                = "miniblue-vm-key"
  resource_group_name = azurerm_resource_group.example.name
  location            = azurerm_resource_group.example.location
  public_key          = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCOE+9GurQjAwCpvgzUKwlrOwYWyj8B/zgWiNcxjEt+XJ+JsmspLcZU1wqZZqKA+syHGH8UQR2PE0sElRvwD/E34lVzBL+2+TEiml6R3dJsq0IXvXd7S1+8Ox5qSnPZG0W/v0x6dufZRPpG5sOIPWQihyS5GoZHMfm2i7N+fKy/5YhRPDKZ1hM8ag47uKQigWTOJmo1ip2haLePrvUKlEYrM/9cVU1dY5iRdhQckVGX/Pg3FPUYAaist9C4ltMHSQJnKDTibtNVavTv7S2ycO8bzeO8wklmmx7UYBiw/SjtRRFpyeGR6Pxe/1CM7n8Q0boCGOTZQg4OyHJJN9TJrP/v miniblue-example"
}

resource "azurerm_linux_virtual_machine" "example" {
  name                = "miniblue-vm"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name
  size                = "Standard_B1s"
  admin_username      = "azureuser"

  network_interface_ids = [azurerm_network_interface.example.id]

  disable_password_authentication = true

  admin_ssh_key {
    username   = "azureuser"
    public_key = azurerm_ssh_public_key.example.public_key
  }

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "ubuntu"
    sku       = "22_04-lts"
    version   = "latest"
  }

  # Runs once on create inside the VM container. Only shell scripts are
  # supported; a #cloud-config payload is skipped.
  custom_data = base64encode(<<-EOT
    #!/bin/sh
    echo "hello from miniblue" > /tmp/miniblue-custom-data
  EOT
  )

  tags = {
    environment = "example"
  }
}

resource "azurerm_managed_disk" "data" {
  name                 = "miniblue-vm-data"
  location             = azurerm_resource_group.example.location
  resource_group_name  = azurerm_resource_group.example.name
  storage_account_type = "Standard_LRS"
  create_option        = "Empty"
  disk_size_gb         = 32
}

resource "azurerm_virtual_machine_data_disk_attachment" "data" {
  managed_disk_id    = azurerm_managed_disk.data.id
  virtual_machine_id = azurerm_linux_virtual_machine.example.id
  lun                = 10
  caching            = "ReadWrite"
}

resource "azurerm_virtual_machine_extension" "example" {
  name                 = "miniblue-vm-extension"
  virtual_machine_id   = azurerm_linux_virtual_machine.example.id
  publisher            = "Microsoft.Azure.Extensions"
  type                 = "CustomScript"
  type_handler_version = "2.1"

  settings = jsonencode({
    commandToExecute = "echo extension metadata only"
  })
}
