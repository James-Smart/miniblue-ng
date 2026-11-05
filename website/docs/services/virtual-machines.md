# Virtual Machines

miniblue emulates Azure Virtual Machines (`Microsoft.Compute/virtualMachines`) via ARM endpoints. When Docker is available on the host, each VM is backed by a real Docker container on its own per-VM network. Without Docker the lifecycle metadata still works, and any operation that needs a running container returns `409 DockerUnavailable`.

A VM is more than a single container. You can attach network interfaces and managed disks, declare extensions and SSH public keys, deploy named service containers onto it, stream their logs, run commands inside it, open an interactive shell and attach managed identities for token attestation.

## Docker detection

miniblue probes for the `docker` CLI at startup and runs `docker info`. The log shows:

```
[vm] Docker is available
```

or it falls back to stub mode when the CLI is absent or the daemon is unreachable. Set `MINIBLUE_DISABLE_DOCKER=1` to force stub mode even on a machine that has Docker.

## Image mapping

VMs boot a Linux container image. miniblue resolves the image in this order:

1. `properties.miniblue.image` is an explicit container image and the recommended escape hatch (for example `nginx:alpine`).
2. `properties.storageProfile.imageReference` is mapped through an alias table.
3. With no image reference at all, the default is `ubuntu:24.04`.

| `imageReference` contains | Container image |
|---------------------------|-----------------|
| `24.04` / `2404` / `24_04` | `ubuntu:24.04` |
| `22.04` / `2204` / `22_04` | `ubuntu:22.04` |
| `ubuntu` (any other) | `ubuntu:24.04` |
| `debian` | `debian:stable-slim` |

An `imageReference` that is present but cannot be mapped fails loudly, so a typo does not silently boot the wrong OS. Set `properties.miniblue.image` to bypass the table.

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `.../Microsoft.Compute/virtualMachines/{name}` | Create or update a VM |
| `PATCH` | `.../Microsoft.Compute/virtualMachines/{name}` | Partially update a VM (merge) |
| `GET` | `.../Microsoft.Compute/virtualMachines/{name}` | Get a VM (live power state and instance view) |
| `DELETE` | `.../Microsoft.Compute/virtualMachines/{name}` | Delete a VM, its services and network |
| `GET` | `.../Microsoft.Compute/virtualMachines` | List VMs in a resource group |
| `GET` | `/subscriptions/{sub}/providers/Microsoft.Compute/virtualMachines` | List VMs across the subscription |
| `POST` | `.../virtualMachines/{name}/start` | Start the VM and its services |
| `POST` | `.../virtualMachines/{name}/powerOff` | Stop the VM and its services |
| `POST` | `.../virtualMachines/{name}/deallocate` | Deallocate the VM (metadata-only without Docker) |
| `POST` | `.../virtualMachines/{name}/restart` | Restart the VM and its services |
| `POST` | `.../virtualMachines/{name}/runCommand` | Run a command inside the VM |
| `GET` | `.../virtualMachines/{name}/instanceView` | Get the instance view object |
| `GET` | `.../virtualMachines/{name}/vmSizes` | List the VM size catalogue |
| `GET` | `.../virtualMachines/{name}/logs` | Read service logs (`?service=`, `?tail=N`, `?follow=true`) |
| `GET` | `.../virtualMachines/{name}/services` | List deployed services |
| `PUT` | `.../virtualMachines/{name}/services/{service}` | Deploy or replace a service |
| `GET` | `.../virtualMachines/{name}/services/{service}` | Get a service |
| `DELETE` | `.../virtualMachines/{name}/services/{service}` | Remove a service |
| `GET` | `.../virtualMachines/{name}/extensions` | List extensions |
| `PUT` | `.../virtualMachines/{name}/extensions/{extension}` | Create or update an extension |
| `GET` | `.../virtualMachines/{name}/extensions/{extension}` | Get an extension |
| `PATCH` | `.../virtualMachines/{name}/extensions/{extension}` | Merge extension settings and tags |
| `DELETE` | `.../virtualMachines/{name}/extensions/{extension}` | Delete an extension |

Paths are abbreviated; the full prefix is `/subscriptions/{sub}/resourceGroups/{rg}/providers`.

## Create a VM

```bash
azlocal group create --name myRG --location eastus
azlocal vm create --resource-group myRG --name web01 --image ubuntu:24.04
```

or via the ARM API, with a network interface and an SSH key:

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Compute/virtualMachines/web01" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "properties": {
      "hardwareProfile": { "vmSize": "Standard_B1s" },
      "storageProfile": {
        "imageReference": { "publisher": "Canonical", "offer": "ubuntu", "sku": "22_04-lts" },
        "osDisk": { "createOption": "FromImage", "diskSizeGB": 30 }
      },
      "osProfile": {
        "computerName": "web01",
        "adminUsername": "azureuser",
        "linuxConfiguration": {
          "disablePasswordAuthentication": true,
          "ssh": { "publicKeys": [
            { "path": "/home/azureuser/.ssh/authorized_keys", "keyData": "ssh-rsa AAAA... miniblue" }
          ] }
        }
      },
      "networkProfile": {
        "networkInterfaces": [
          { "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Network/networkInterfaces/nic-web01", "properties": { "primary": true } }
        ]
      }
    }
  }'
```

Response (`201 Created`) follows the ARM envelope with `provisioningState: Succeeded` and a `miniblue.powerState` field. When Docker is available the create starts a keep-alive container on a per-VM network. A VM whose container fails to start fails fast with `409 ContainerStartFailed` and persists nothing, unlike ACI which falls back to a stub.

## Read responses

Every VM `GET` and `PUT`/`PATCH` response includes:

- `properties.vmId` — a stable UUID for the VM.
- `properties.provisioningState` — always `Succeeded`.
- `properties.instanceView` — the ARM status block described below.
- `properties.storageProfile.imageReference`, `osDisk` and `dataDisks` as stored.
- `properties.osProfile`, including the supplied `linuxConfiguration`, SSH keys and `customData`, but never `adminPassword`.
- `properties.networkProfile.networkInterfaces` in the order supplied, with the first marked `primary` by the caller.
- `properties.miniblue` — image, power state, container name and custom-data status.

`GET .../instanceView` returns the same object unwrapped (no `properties` wrapper):

```json
{
  "statuses": [
    { "code": "ProvisioningState/succeeded", "level": "Info", "displayStatus": "Provisioning succeeded", "time": "2026-11-05T00:00:00Z" },
    { "code": "PowerState/running", "level": "Info", "displayStatus": "VM running" }
  ],
  "vmAgent": {
    "vmAgentVersion": "2.0.0",
    "statuses": [{ "code": "ProvisioningState/succeeded", "level": "Info", "displayStatus": "Ready" }]
  },
  "disks": [
    { "name": "web01_OsDisk_1_1a2b3c4d", "statuses": [{ "code": "ProvisioningState/succeeded", "level": "Info", "displayStatus": "Provisioning succeeded" }] }
  ]
}
```

The status list reflects the live power state (`PowerState/running`, `PowerState/stopped` or `PowerState/deallocated`) and is recomputed on every read, so it cannot go stale after a power action. `?$expand=instanceView` is accepted; the instance view is always included either way.

`GET .../vmSizes` returns `{"value": [...]}` with a broad catalogue of common sizes (`Standard_B1s`, `Standard_B2s`, `Standard_D2s_v3`, `Standard_DS1_v2`, ...). miniblue does not enforce sizes — the endpoint exists so the provider can decide whether it must deallocate before a resize.

## Managed disks and OS disk details

The managed-disk resource lives under `Microsoft.Compute/disks`:

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `.../Microsoft.Compute/disks/{name}` | Create or update a disk |
| `PATCH` | `.../Microsoft.Compute/disks/{name}` | Partially update a disk |
| `GET` | `.../Microsoft.Compute/disks/{name}` | Get a disk |
| `DELETE` | `.../Microsoft.Compute/disks/{name}` | Delete an unattached disk |
| `GET` | `.../Microsoft.Compute/disks` | List disks in a resource group |
| `GET` | `/subscriptions/{sub}/providers/Microsoft.Compute/disks` | List disks in the subscription |

Creating a VM without an explicit OS disk makes miniblue generate one named `<vmName>_OsDisk_1_<8 hex>` in the VM's resource group, using the requested `osDisk.diskSizeGB`, managed-disk `storageAccountType` and `createOption` (`FromImage` with the image reference by default). `GET` returns the effective `name`, `diskSizeGB`, `storageAccountType`, `managedDisk.id`, `osType`, `createOption` and `caching` (`ReadWrite` by default), so the provider can read back what it wrote.

- Supplying `storageProfile.osDisk.managedDisk.id` attaches an existing disk. A missing disk returns `404 DiskNotFound`; a disk attached to another VM returns `409 DiskInUse`.
- Changing the OS disk reference on `PUT`/`PATCH` attaches the new disk and detaches the old one. An explicit `null` keeps the existing disk rather than orphaning it.
- Data disks are declared in `storageProfile.dataDisks`. An entry that references a `managedDisk.id` that does not exist yet is created with `createOption: Attach` and attached; entries removed from the array are detached.
- Deleting a VM detaches (but does not delete) its OS and data disks. Deleting a disk that is still attached returns `409 DiskAttached`.
- Deleting a resource group removes every `disk:` record along with the rest of the group.

## Network interfaces

Network interfaces are `Microsoft.Network/networkInterfaces` resources, created independently and referenced from `properties.networkProfile.networkInterfaces`. On VM create/update miniblue sets the NIC's `properties.virtualMachine.id`; on VM delete, or when a NIC is removed from the profile, that link is cleared. NICs are never deleted with the VM — Terraform deletes `azurerm_network_interface` separately. See [Virtual Networks](virtual-networks.md#network-interfaces) for the endpoint list and allocation rules.

Missing or malformed NIC references are ignored rather than rejected, matching the permissive style of the network service.

## PATCH and update semantics

`PATCH` applies merge semantics to an existing VM and returns `200`. It never creates a resource, so a missing VM returns `404`.

- Maps merge recursively, arrays replace, scalars replace and JSON `null` removes a key.
- `location`, `tags`, `zones`, `plan` and `extendedLocation` replace when present.
- `identity` replaces when present; `null` removes all identity assignments.
- Generated fields (`id`, `vmId`, `miniblue`, `instanceView`, `provisioningState`) are never taken from the request.
- NIC links, OS-disk replacement and data-disk attachments are re-synced after the merge.
- `PUT` on an existing VM uses the same merge path and returns `200`; `PUT` on a new VM returns `201`.
- Custom data is create-time input only. Changing it on an update does not re-run it.

```bash
curl -X PATCH "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Compute/virtualMachines/web01" \
  -H "Content-Type: application/json" \
  -d '{"tags":{"env":"test"},"properties":{"hardwareProfile":{"vmSize":"Standard_B2s"}}}'
```

## Power actions

```bash
azlocal vm stop    --resource-group myRG --name web01
azlocal vm start   --resource-group myRG --name web01
azlocal vm restart --resource-group myRG --name web01

# The ARM API also supports deallocate
curl -X POST "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Compute/virtualMachines/web01/deallocate"
```

Power actions cascade to every service container on the VM. `powerState` on a `GET` is refreshed live from `docker inspect`.

### Docker-free power actions

Without Docker there is no runtime to control, but power actions are still meaningful metadata: Terraform power-cycles VMs during updates and Docker-free CI must not fail those calls. `start`, `restart`, `powerOff` and `deallocate` update the stored power state (`running` or `stopped`) and return `200`. The split is deliberate:

| Operation | Without Docker |
|-----------|----------------|
| `start`, `restart`, `powerOff`, `deallocate` | `200`, metadata updated |
| Service list/delete | `200`, metadata-only |
| `runCommand`, logs, service deploy, SSH | `409 DockerUnavailable` |

## Custom data

`properties.osProfile.customData` (or `properties.userData` as a fallback) is treated as a create-time shell script. The normal encoding is base64, but raw script text, standard base64 and unpadded URL-safe base64 are all accepted. A UTF-8 BOM is trimmed and CRLF is normalised before execution.

```bash
CUSTOM_DATA=$(printf '#!/bin/sh\necho "hello from miniblue" > /tmp/custom-data\n' | base64 -w0)

curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Compute/virtualMachines/web01" \
  -H "Content-Type: application/json" \
  -d '{"location":"eastus","properties":{"osProfile":{"customData":"'"$CUSTOM_DATA"'"}}}'
```

The script runs inside the VM container with `docker exec <container> sh -c <script>` after the container starts, so shell syntax, pipes and environment expansion work.

- Only shell scripts are supported. A payload whose trimmed text starts with `#cloud-config` is skipped.
- The run happens on create only. Updates never re-run custom data.
- A non-zero exit does not fail provisioning, matching Azure's cloud-init behaviour.
- Output is truncated to 16 KiB.

The outcome is recorded under `properties.miniblue.customData` and surfaced as a provisioning event:

```json
{
  "executed": true,
  "exitCode": 0,
  "output": "hello from miniblue\n",
  "shell": "sh"
}
```

Without Docker, creation records `{"executed": false, "reason": "docker unavailable"}`. A `#cloud-config` payload records `{"executed": false, "reason": "cloud-config is not supported; scripts only"}`.

## Extensions

Extensions are metadata-only. miniblue records what the provider asked for but never runs `CustomScript` or any other extension handler.

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Compute/virtualMachines/web01/extensions/bootstrap" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "properties": {
      "publisher": "Microsoft.Azure.Extensions",
      "type": "CustomScript",
      "typeHandlerVersion": "2.1",
      "settings": { "commandToExecute": "echo hello" }
    }
  }'
```

The response echoes `publisher`, `type`, `typeHandlerVersion`, `autoUpgradeMinorVersion`, `enableAutomaticUpgrade`, `settings`, `suppressFailures`, `provisionAfterExtensions` and `protectedSettingsFromKeyVault`, and adds a synthesized `instanceView` with a succeeded status. `protectedSettings` is accepted but never returned. Extensions are children of the VM: deleting the VM or its resource group removes them.

## SSH public keys

SSH public keys are `Microsoft.Compute/sshPublicKeys` resources:

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `.../Microsoft.Compute/sshPublicKeys/{name}` | Create or update a key |
| `PATCH` | `.../Microsoft.Compute/sshPublicKeys/{name}` | Update tags or `publicKey` |
| `GET` | `.../Microsoft.Compute/sshPublicKeys/{name}` | Get a key |
| `DELETE` | `.../Microsoft.Compute/sshPublicKeys/{name}` | Delete a key |
| `POST` | `.../Microsoft.Compute/sshPublicKeys/{name}/generateKeyPair` | Generate a key pair |
| `GET` | `.../Microsoft.Compute/sshPublicKeys` | List keys in a resource group |
| `GET` | `/subscriptions/{sub}/providers/Microsoft.Compute/sshPublicKeys` | List keys in the subscription |

If the request body supplies `properties.publicKey`, it is echoed and preserved across `PATCH` calls that omit it. Otherwise miniblue generates a real 2048-bit RSA key at create time. `POST .../generateKeyPair` returns a fresh pair without replacing the stored public key:

```json
{
  "publicKey": "ssh-rsa AAAA... miniblue",
  "privateKey": "-----BEGIN RSA PRIVATE KEY-----..."
}
```

The key resource is not installed into the container. `osProfile.linuxConfiguration.ssh.publicKeys` on the VM is echoed for provider compatibility; use `azlocal vm ssh` for shell access.

## Deploy services

A service is a named container that runs on the VM's network with host-published ports.

```bash
azlocal vm deploy --resource-group myRG --name web01 \
  --service api --image nginx:alpine --ports 8080:80 \
  --env LOG_LEVEL=debug --env REGION=local
```

Deploying a service whose host port clashes with a sibling returns `409 PortConflict`. Re-deploying the same service name replaces the existing container. List and remove services with:

```bash
azlocal vm services       --resource-group myRG --name web01
azlocal vm service-delete --resource-group myRG --name web01 --service api
```

## Logs

```bash
# combined, labelled view of every service
azlocal vm logs --resource-group myRG --name web01

# a single service, last 100 lines
azlocal vm logs --resource-group myRG --name web01 --service api --tail 100

# live stream
azlocal vm logs --resource-group myRG --name web01 --follow
```

Combined output prefixes each line with `[service]`. Streaming uses chunked `text/plain` and ends cleanly when the client disconnects.

## Run commands and SSH

```bash
# one-off command, mirrors Azure runCommand
azlocal vm run-command --resource-group myRG --name web01 --command "uname -a"

# interactive shell (docker exec -it, /bin/bash with /bin/sh fallback)
azlocal vm ssh --resource-group myRG --name web01
```

`runCommand` captures stdout, stderr and the exit code. Both require a running container and return `409 VMNotRunning` or `409 DockerUnavailable` otherwise. `vm ssh` shells out to `docker exec` directly, so it needs Docker on the same host as the CLI.

## Managed identity attestation

User-assigned managed identities live under `Microsoft.ManagedIdentity/userAssignedIdentities` and can be attached to a VM. A workload inside the VM (or its services) then obtains a token with no code changes through the standard `IDENTITY_ENDPOINT` / `IDENTITY_HEADER` protocol that Azure SDK credential chains already probe.

```bash
azlocal identity create --resource-group myRG --name app-id
azlocal vm identity-assign --resource-group myRG --name web01 --identity app-id

# from inside the VM, the SDK or a curl call hits the token endpoint
curl "http://localhost:4566/metadata/identity/oauth2/token?resource=https://management.azure.com/"
```

Tokens carry `xms_mirid` and VM claims and are verifiable via `POST /metadata/identity/introspect`. The signature is a clear mock (`alg: none`); validation is by store lookup, not by trusting token contents. Remove an assignment with `azlocal vm identity-remove`.

## Enabling only what you need

The VM feature is split across four filterable services: `vm` (VMs and extensions), `network` (VNets, subnets and NICs), `disks` (managed disks) and `sshkeys` (SSH public keys). When `SERVICES` is set, enable all of them or the VM calls will fail on missing dependencies:

```bash
SERVICES=vm,network,disks,sshkeys ./bin/miniblue
```

Add `resourcegroups` to that list if you create resource groups through the API rather than using pre-seeded state. The managed-identity services (`identity`, plus the always-registered metadata/auth endpoints) are only needed if you use identity assignment.

## Terraform

The `azurerm` provider resources for this surface are:

- `azurerm_network_interface`
- `azurerm_managed_disk` and `azurerm_virtual_machine_data_disk_attachment`
- `azurerm_ssh_public_key`
- `azurerm_linux_virtual_machine`
- `azurerm_virtual_machine_extension`

See [examples/terraform/vm](https://github.com/moabukar/miniblue/tree/main/examples/terraform/vm) for a generic fixture that creates a resource group, VNet/subnet, public IP, NSG, NIC, SSH key, Linux VM, managed data disk and extension.

## azlocal command reference

```
azlocal vm create|list|show|delete
azlocal vm start|stop|restart
azlocal vm deploy|services|service-delete
azlocal vm logs            [--service NAME] [--tail N] [--follow]
azlocal vm run-command     --command "..."
azlocal vm ssh
azlocal vm identity-assign|identity-remove --identity NAME
azlocal identity create|list|show|delete
```

## Limitations

- The VM is a Linux container, not a real virtual machine. There is no kernel isolation, no Windows images and no cloud-init.
- The keep-alive container runs `sleep`. VM extensions are metadata-only; they are never executed. Custom data runs as a shell script only once, on create.
- SSH public keys are registered as resources and echoed on the VM profile, but are not installed into the container.
- VM sizes are not enforced; `vmSizes` is a static superset used by the provider for resize pre-checks.
- Without Docker, lifecycle metadata works but `runCommand`, logs, service deploy and SSH return `409 DockerUnavailable`. Power actions still succeed.
- Container and network names are derived from the resource group and VM name, so those must be valid Docker identifiers.
