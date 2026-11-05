# Virtual Networks

miniblue emulates Azure Virtual Networks (VNets), subnets and network interfaces (NICs) via the ARM API. These endpoints are compatible with the Terraform `azurerm_virtual_network`, `azurerm_subnet` and `azurerm_network_interface` resources.

## API endpoints

### Virtual Networks

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/virtualNetworks/{name}` | Create or update |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/virtualNetworks/{name}` | Get |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/virtualNetworks/{name}` | Delete |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/virtualNetworks` | List |

### Subnets

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `.../virtualNetworks/{vnet}/subnets/{subnet}` | Create or update |
| `GET` | `.../virtualNetworks/{vnet}/subnets/{subnet}` | Get |
| `DELETE` | `.../virtualNetworks/{vnet}/subnets/{subnet}` | Delete |
| `GET` | `.../virtualNetworks/{vnet}/subnets` | List |

### Network Interfaces

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/networkInterfaces/{name}` | Create or update |
| `PATCH` | `.../networkInterfaces/{name}` | Partially update |
| `GET` | `.../networkInterfaces/{name}` | Get |
| `DELETE` | `.../networkInterfaces/{name}` | Delete |
| `GET` | `.../networkInterfaces` | List in a resource group |
| `GET` | `/subscriptions/{sub}/providers/Microsoft.Network/networkInterfaces` | List in the subscription |

## Create a VNet

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/virtualNetworks/my-vnet?api-version=2020-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "properties": {
      "addressSpace": {
        "addressPrefixes": ["10.0.0.0/16"]
      }
    }
  }'
```

Response (`201 Created`):

```json
{
  "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/virtualNetworks/my-vnet",
  "name": "my-vnet",
  "type": "Microsoft.Network/virtualNetworks",
  "location": "eastus",
  "etag": "W/\"miniblue\"",
  "properties": {
    "provisioningState": "Succeeded",
    "resourceGuid": "00000000-0000-0000-0000-000000000000",
    "addressSpace": {
      "addressPrefixes": ["10.0.0.0/16"]
    },
    "dhcpOptions": {
      "dnsServers": []
    },
    "subnets": [],
    "virtualNetworkPeerings": [],
    "enableDdosProtection": false,
    "enableVmProtection": false
  }
}
```

## Get a VNet

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/virtualNetworks/my-vnet?api-version=2020-06-01"
```

The response includes a `subnets` array populated from the store.

## Create a subnet

The parent VNet must exist first.

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/virtualNetworks/my-vnet/subnets/my-subnet?api-version=2020-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "properties": {
      "addressPrefix": "10.0.1.0/24"
    }
  }'
```

Response (`201 Created`):

```json
{
  "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/virtualNetworks/my-vnet/subnets/my-subnet",
  "name": "my-subnet",
  "etag": "W/\"miniblue\"",
  "type": "Microsoft.Network/virtualNetworks/subnets",
  "properties": {
    "provisioningState": "Succeeded",
    "addressPrefix": "10.0.1.0/24",
    "addressPrefixes": ["10.0.1.0/24"],
    "serviceEndpoints": [],
    "delegations": [],
    "privateEndpointNetworkPolicies": "Disabled",
    "privateLinkServiceNetworkPolicies": "Enabled",
    "defaultOutboundAccess": true
  }
}
```

## List subnets

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/virtualNetworks/my-vnet/subnets?api-version=2020-06-01"
```

## Delete a subnet

```bash
curl -X DELETE "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/virtualNetworks/my-vnet/subnets/my-subnet?api-version=2020-06-01"
```

## Delete a VNet

```bash
curl -X DELETE "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/virtualNetworks/my-vnet?api-version=2020-06-01"
```

Deleting a VNet also deletes all its subnets.

## Network interfaces

A NIC joins a subnet and optionally references a public IP address, a network security group and load-balancer backend pools. References are echoed without requiring the target resource to exist, so ordering and casing traps are avoided.

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/networkInterfaces/nic-web01?api-version=2020-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "properties": {
      "ipConfigurations": [
        {
          "name": "ipconfig1",
          "properties": {
            "privateIPAllocationMethod": "Dynamic",
            "subnet": { "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/virtualNetworks/my-vnet/subnets/my-subnet" },
            "publicIPAddress": { "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/publicIPAddresses/my-ip" }
          }
        }
      ],
      "networkSecurityGroup": { "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/networkSecurityGroups/my-nsg" }
    }
  }'
```

Response (`201 Created`), abbreviated:

```json
{
  "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/networkInterfaces/nic-web01",
  "name": "nic-web01",
  "type": "Microsoft.Network/networkInterfaces",
  "location": "eastus",
  "etag": "W/\"miniblue\"",
  "properties": {
    "provisioningState": "Succeeded",
    "resourceGuid": "...",
    "macAddress": "00-11-22-33-44-55",
    "primary": true,
    "enableAcceleratedNetworking": false,
    "enableIPForwarding": false,
    "dnsSettings": {
      "dnsServers": [],
      "appliedDnsServers": [],
      "internalFqdn": "nic-web01.<suffix>",
      "internalDomainNameSuffix": "myrg.<hex>.internal.cloudapp.net"
    },
    "networkSecurityGroup": { "id": ".../networkSecurityGroups/my-nsg" },
    "ipConfigurations": [
      {
        "id": ".../networkInterfaces/nic-web01/ipConfigurations/ipconfig1",
        "name": "ipconfig1",
        "type": "Microsoft.Network/networkInterfaces/ipConfigurations",
        "etag": "W/\"miniblue\"",
        "properties": {
          "provisioningState": "Succeeded",
          "privateIPAddress": "10.0.1.4",
          "privateIPAllocationMethod": "Dynamic",
          "privateIPAddressVersion": "IPv4",
          "primary": true,
          "subnet": { "id": ".../subnets/my-subnet" },
          "publicIPAddress": { "id": ".../publicIPAddresses/my-ip" },
          "applicationGatewayBackendAddressPools": [],
          "loadBalancerBackendAddressPools": [],
          "loadBalancerInboundNatRules": []
        }
      }
    ]
  }
}
```

Rules:

- With no `ipConfigurations`, miniblue creates `ipconfig1` with dynamic IPv4 allocation.
- A dynamic private IP is allocated from the referenced subnet's first address prefix, starting at the first usable host after `.3` and skipping addresses already used by other NICs in that subnet. If the subnet is absent or its prefix cannot be parsed, addresses are allocated from `10.0.0.0/24` instead.
- `privateIPAllocationMethod: Static` with a supplied `privateIPAddress` keeps that address.
- `resourceGuid`, `macAddress`, the allocated private IP, `dnsSettings.internalDomainNameSuffix`, `dnsSettings.appliedDnsServers`, generated IP-configuration IDs and the `virtualMachine` link are generated once and preserved across `PUT`/`PATCH`. A NIC can be patched while attached.
- `GET`/`PATCH`/`PUT` do not require the referenced subnet, public IP or NSG records to exist.

### VM linkage

When a VM's `properties.networkProfile.networkInterfaces` references a NIC, miniblue sets that NIC's `properties.virtualMachine.id` to the VM's ARM ID. The link is cleared when the VM is deleted or the NIC is removed from the profile. NICs themselves are not deleted with the VM; Terraform deletes `azurerm_network_interface` separately.

## azlocal

```bash
# Create
azlocal network vnet create --name my-vnet --resource-group myRG \
  --address-prefix 10.0.0.0/16 --location eastus

# Show
azlocal network vnet show --name my-vnet --resource-group myRG

# List
azlocal network vnet list --resource-group myRG

# Delete
azlocal network vnet delete --name my-vnet --resource-group myRG
```

## Terraform

```hcl
resource "azurerm_virtual_network" "example" {
  name                = "example-vnet"
  address_space       = ["10.0.0.0/16"]
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name
}

resource "azurerm_subnet" "example" {
  name                 = "example-subnet"
  resource_group_name  = azurerm_resource_group.example.name
  virtual_network_name = azurerm_virtual_network.example.name
  address_prefixes     = ["10.0.1.0/24"]
}

resource "azurerm_network_interface" "example" {
  name                = "example-nic"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name

  ip_configuration {
    name                          = "ipconfig1"
    subnet_id                     = azurerm_subnet.example.id
    private_ip_address_allocation = "Dynamic"
  }
}
```

See the [Terraform guide](../guides/terraform.md) for full provider configuration.
