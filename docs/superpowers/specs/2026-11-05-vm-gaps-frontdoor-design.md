# VM gaps and Front Door support

Date: 2026-11-05
Status: proposed

## Intent

miniblue should be able to run a real `azurerm` OpenTofu/Terraform configuration
end-to-end against a local endpoint: `init`, `plan`, `apply`, a second `plan`
with no drift, and `destroy`. The work below closes the VM gaps that block
`azurerm_linux_virtual_machine` and adds Azure Front Door Standard/Premium.

This is deliberately not shaped around one user's configuration. The target is
the general provider-facing lifecycle for each listed resource: write, read,
list where the provider lists, update/patch where mutable, delete, and cascade
where Azure cascades. Tests use our own representative fixtures.

## Goals

- `azurerm_linux_virtual_machine` can be created, refreshed, updated and
  destroyed, including a NIC, an OS disk, data disks, extensions, SSH public
  keys and script-only custom data.
- The provider can read back believable generated values: private/public IPs,
  MAC address, VM power state, disk size/SKU/attachment state, extension
  status.
- Azure Front Door Standard/Premium resources can be created, read, updated
  and destroyed across the full profile tree, including custom domains,
  secrets, security policies and the Network-provider WAF policy.
- The same behaviour is available with `SERVICES` filtering and survives
  resource-group cascade deletion.
- Documentation and the parity matrix describe the new surface.

## Non-goals

- Real VM compute, real disk I/O, Windows VMs, actual cloud-init, real
  extension execution, or real container image boot semantics beyond the
  existing VM container.
- Real Front Door traffic routing, TLS termination, DNS validation, WAF
  enforcement, metrics or logs.
- Private endpoints, private link origins, managed certificates, or Front Door
  data-plane APIs.
- Matching every Azure validation rule or error code.

## Anti-goals

- Adding special cases keyed to one Terraform configuration.
- Returning empty or zero-valued fields that make the provider panic or show
  perpetual drift.
- Silently accepting writes that later disappear from a GET.

## Compatibility notes

The design covers provider behaviour observed in `hashicorp/azurerm` v3 and
v4-era resources:

- VM reads may request `?$expand=UserData` or `?$expand=instanceView`; both
  must work, and `properties.instanceView` should always be present because
  older provider paths read power state from it.
- VM updates call the separate `GET .../virtualMachines/{name}/instanceView`
  endpoint and `GET .../virtualMachines/{name}/vmSizes`.
- VM data-disk attachment GETs the VM and PUTs the whole model back, so the VM
  response must round-trip fields it does not understand.
- Front Door custom-domain association is client-side: the association resource
  has no dedicated Azure API. It checks the custom domain and the routes'
  `properties.customDomains` arrays, and its delete removes the domain from the
  route. Supporting routes and custom domains correctly is enough.

## Package layout

Follow the existing per-service handler pattern under `internal/services/`.

| Area | Package | Registered service name |
|------|---------|-------------------------|
| NICs | extend `internal/services/network` (`nic.go`) | `network` |
| Managed disks | new `internal/services/disks` | `disks` |
| VM extensions | extend `internal/services/vm` (`extensions.go`) | `vm` |
| SSH public keys | new `internal/services/sshkeys` | `sshkeys` |
| Front Door + WAF | new `internal/services/frontdoor` | `frontdoor` |

Add the three new services to `internal/server/server.go`. NICs and extensions
stay with their parent services so `SERVICES=network` and `SERVICES=vm` remain
meaningful.

## Shared conventions

- Store records as `map[string]interface{}` in the existing `store.Store`,
  mirroring the current handlers. Use typed structs only for request parsing
  where it materially helps.
- Use `azerr.NotFound`, `azerr.BadRequest`, `azerr.WriteError` for errors.
- HTTP semantics: `PUT` returns `201` on create and `200` on replace/update;
  `PATCH` returns `200`; `GET` returns `200` or `404`; list returns
  `{"value": [...]}`; `DELETE` returns `202` (or `204` for VMs, matching the
  existing VM handler). Deletes are synchronous in the store.
- Every response includes the ARM envelope: `id`, `name`, `type`, `location`,
  `tags`, `etag` where the existing services do, and
  `properties.provisioningState: "Succeeded"`.
- `PUT` and `PATCH` both merge over an existing record: maps merge, arrays
  replace, and JSON `null` removes a key. Scalars replace. `PUT` additionally
  preserves generated/read-only fields unless the request explicitly supplies
  a new value: resource IDs, `resourceGuid`, `macAddress`, allocated private
  IPs, `virtualMachine` links, `timeCreated`, `managedBy`, `diskState`,
  `diskSizeBytes`, generated public keys, Front Door hostnames and validation
  tokens. This mirrors the provider practice of reading a resource and writing
  the whole model back after changing one field. `PATCH` uses the same merge
  path.
- Internal fields are prefixed `_` and stripped before responses (reuse the VM
  `publicView` approach or a local equivalent).
- Generated values (IPs, MAC addresses, hostnames, tokens, disk names) are
  generated once and persisted; they must not change between GETs.

---

# Part 1: VM gaps

## 1. Network interfaces

### Routes

Add to `internal/services/network/handler.go`'s existing `Register`:

```
/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/networkInterfaces
  GET                         list in resource group
  /{nicName}
    PUT, GET, PATCH, DELETE
/subscriptions/{sub}/providers/Microsoft.Network/networkInterfaces
  GET                         list in subscription
```

### Store

Key: `nic:<sub>:<rg>:<name>`.

### Response shape

Return the request's accepted fields plus defaults:

```json
{
  "id": ".../Microsoft.Network/networkInterfaces/nic1",
  "name": "nic1",
  "type": "Microsoft.Network/networkInterfaces",
  "location": "eastus",
  "tags": {},
  "etag": "W/\"miniblue\"",
  "properties": {
    "provisioningState": "Succeeded",
    "resourceGuid": "<stable uuid>",
    "macAddress": "00-11-22-33-44-55",
    "primary": true,
    "enableAcceleratedNetworking": false,
    "enableIPForwarding": false,
    "dnsSettings": {
      "dnsServers": [],
      "appliedDnsServers": [],
      "internalDnsNameLabel": "<input if any>",
      "internalFqdn": "<name>.<suffix>",
      "internalDomainNameSuffix": "<rg>.<hex>.internal.cloudapp.net"
    },
    "networkSecurityGroup": { "id": "..." },
    "virtualMachine": { "id": "..." },
    "ipConfigurations": [
      {
        "id": ".../networkInterfaces/nic1/ipConfigurations/ipconfig1",
        "name": "ipconfig1",
        "type": "Microsoft.Network/networkInterfaces/ipConfigurations",
        "etag": "W/\"miniblue\"",
        "properties": {
          "provisioningState": "Succeeded",
          "privateIPAddress": "10.0.1.4",
          "privateIPAllocationMethod": "Dynamic",
          "privateIPAddressVersion": "IPv4",
          "primary": true,
          "subnet": { "id": "..." },
          "publicIPAddress": { "id": "..." },
          "applicationGatewayBackendAddressPools": [],
          "loadBalancerBackendAddressPools": [{ "id": "..." }],
          "loadBalancerInboundNatRules": []
        }
      }
    ]
  }
}
```

Rules:

- Echo `ipConfigurations` from the request, adding `id`/`type`/`etag` and
  defaults. If a config has no name, use `ipconfig1`; if no configs are sent,
  create one with `name: "ipconfig1"`, `privateIPAllocationMethod: "Dynamic"`,
  `privateIPAddressVersion: "IPv4"`, `primary: true`.
- If `privateIPAllocationMethod` is `Static` and a `privateIPAddress` was
  supplied, keep it. Otherwise allocate a free IPv4 address:
  - Parse the referenced subnet's first address prefix from the network store
    (`subnet:<sub>:<rg>:<vnet>:<subnet>`).
  - Use the first usable host after `.3` (e.g. `10.0.1.4`), scanning existing
    NICs in the same subnet and skipping addresses already used.
  - If the prefix cannot be parsed or the subnet is absent, allocate
    `10.0.0.4`, `10.0.0.5`, ... skipping existing allocations.
- `primary` is true for the first config unless the request explicitly marks a
  different one.
- Echo `subnet`, `publicIPAddress`, `networkSecurityGroup`,
  `loadBalancerBackendAddressPools`, `loadBalancerInboundNatRules` and
  `applicationGatewayBackendAddressPools` without requiring the referenced
  resources to exist. This matches the permissive style of the existing
  network handlers and avoids ordering/casing traps.
- `dnsSettings.internalDomainNameSuffix` must be non-empty and stable. The
  provider exposes it as `internal_domain_name_suffix`.
- `virtualMachine.id` is set only when a VM references the NIC. It is cleared
  when the VM is deleted or its `networkProfile` changes.
- `PUT`/`PATCH` preserve `resourceGuid`, `macAddress`, the allocated private
  IP (unless a new static IP is supplied), `virtualMachine.id`,
  `dnsSettings.appliedDnsServers`, `dnsSettings.internalDomainNameSuffix` and
  the generated IP-configuration IDs. The provider commonly GETs a NIC, edits
  one field and PUTs the whole model back. A NIC can be patched while attached.

### VM linkage

- On VM create/PUT/PATCH, for every ID in
  `properties.networkProfile.networkInterfaces`, find the NIC and set
  `properties.virtualMachine.id` to the VM ID. Clear that link on NICs that
  were previously linked to this VM but are no longer listed.
- On VM delete, clear `virtualMachine.id` on every linked NIC. Do not delete the
  NIC; Terraform deletes `azurerm_network_interface` separately.

---

## 2. Managed disks

### Routes

New package `internal/services/disks`:

```
/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/disks
  GET                         list in resource group
  /{diskName}
    PUT, GET, PATCH, DELETE
/subscriptions/{sub}/providers/Microsoft.Compute/disks
  GET                         list in subscription
```

### Store

Key: `disk:<sub>:<rg>:<name>`.

### Response shape

```json
{
  "id": ".../Microsoft.Compute/disks/data1",
  "name": "data1",
  "type": "Microsoft.Compute/disks",
  "location": "eastus",
  "tags": {},
  "sku": { "name": "Standard_LRS", "tier": "Standard" },
  "properties": {
    "provisioningState": "Succeeded",
    "timeCreated": "2026-11-05T00:00:00Z",
    "diskSizeGB": 30,
    "diskSizeBytes": 32212254720,
    "osType": "Linux",
    "hyperVGeneration": "V2",
    "creationData": { "createOption": "Empty" },
    "diskState": "Unattached",
    "managedBy": "",
    "diskIOPSReadWrite": 500,
    "diskMBpsReadWrite": 60,
    "networkAccessPolicy": "AllowAll",
    "publicNetworkAccess": "Enabled",
    "encryption": { "type": "EncryptionAtRestWithPlatformKey" }
  }
}
```

Rules:

- `creationData` is echoed from the request; default
  `{"createOption":"Empty"}`.
- `diskSizeGB` defaults to 30 when omitted; `diskSizeBytes` is derived.
- `sku` defaults to `Standard_LRS`/`Standard`; echo the requested SKU.
- `publicNetworkAccess` **must always be present** and `"Enabled"` by default.
  The provider dereferences it unconditionally.
- `networkAccessPolicy` defaults to `"AllowAll"`.
- `diskState`/`managedBy` reflect attachment. `managedBy` is the VM ID when
  attached, empty otherwise.
- `PUT`/`PATCH` support tags, SKU, `diskSizeGB`, `networkAccessPolicy`,
  `publicNetworkAccess`, `diskIOPSReadWrite`, `diskMBpsReadWrite`. Preserve
  `timeCreated`, `managedBy`, `diskState`, `diskSizeBytes`, `provisioningState`
  and the generated defaults when the request omits them; an attached disk
  stays attached across a tag/size update.
- `DELETE` returns `409 DiskAttached` if `managedBy` is non-empty. The provider
  deletes the VM first.
- List responses include the same computed fields.

### OS disk lifecycle

When a Linux VM is created:

- If `storageProfile.osDisk.managedDisk.id` is present, require that disk to
  exist (404 `DiskNotFound` otherwise), attach it (`managedBy` = VM ID,
  `diskState` = `Attached`), and leave ownership with the disk resource.
- If no managed-disk ID is present, generate a stable name:
  `<vmName>_OsDisk_1_<8 lowercase hex>`, truncated to 80 characters. Create a
  disk record in the same resource group:
  - `osType: "Linux"`
  - `sku.name` from `osDisk.managedDisk.storageAccountType`, default
    `Standard_LRS`
  - `diskSizeGB` from `osDisk.diskSizeGB`, default 30
  - `creationData.createOption` from `osDisk.createOption`, default
    `FromImage`; carry over the VM image reference when present
  - `managedBy` = VM ID, `diskState` = `Attached`
  - internal `_vmOwned: true`
- Fill the VM response's `storageProfile.osDisk` with `name`,
  `managedDisk.id`, `managedDisk.storageAccountType`, `diskSizeGB`,
  `osType: "Linux"`, `createOption`, and `caching`. The provider reads `name`,
  `diskSizeGB`, `storageAccountType` and the managed-disk ID.
- On VM delete, detach all managed disks (OS and data): clear `managedBy` and
  set `diskState: "Unattached"`. Do **not** delete them. The provider's
  `delete_os_disk_on_deletion` feature then issues the disk DELETE; if the
  feature is off the disk remains, which matches Azure. Resource-group deletion
  removes all disks.
- If a VM PUT/PATCH replaces the OS disk ID, detach the old disk and attach the
  new one. Never let a VM own two OS disks.

### Data disks

- A VM PUT/PATCH containing `storageProfile.dataDisks` replaces that array.
  For every entry with `managedDisk.id`:
  - if the disk exists, set `managedBy` to the VM ID and
    `diskState: "Attached"`;
  - if it does not exist, create it with the provided name, size, SKU and
    `creationData` (`createOption` default `Attach`).
- Disks that were previously attached to this VM but are no longer in the array
  are detached (clear `managedBy`, `diskState: "Unattached"`).
- `azurerm_virtual_machine_data_disk_attachment` GETs then PUTs the whole VM;
  the response must round-trip `storageProfile` exactly.

---

## 3. VM read response additions

Enrich `publicView` / `buildVMRecord` so every GET returns:

- `properties.vmId`: stable UUID.
- `properties.provisioningState: "Succeeded"`.
- `properties.hardwareProfile.vmSize`: input or `Standard_B1s`.
- `properties.storageProfile`: image reference, OS disk (see above), data disks.
- `properties.osProfile`: echo `adminUsername`, `computerName`,
  `allowExtensionOperations`, `linuxConfiguration` (disable password auth,
  SSH keys, patch settings), `customData`, `secrets`.
- `properties.networkProfile.networkInterfaces`: `[{"id": "...", "properties":
  {"primary": true}}]` in the order supplied; first is primary.
- `properties.userData`: echo if supplied (base64 string).
- `properties.instanceView`:
  ```json
  {
    "statuses": [
      {"code": "ProvisioningState/succeeded", "level": "Info",
       "displayStatus": "Provisioning succeeded", "time": "..."},
      {"code": "PowerState/running", "level": "Info",
       "displayStatus": "VM running"}
    ],
    "vmAgent": {
      "vmAgentVersion": "2.0.0",
      "statuses": [{"code": "ProvisioningState/succeeded", "level": "Info",
                    "displayStatus": "Ready"}]
    },
    "disks": [
      {"name": "<os disk name>",
       "statuses": [{"code": "ProvisioningState/succeeded", "level": "Info"}]}
    ]
  }
  ```
  Power states map from the existing internal state:
  `running` → `PowerState/running`, `stopped` → `PowerState/stopped`,
  `deallocated` → `PowerState/deallocated`.
- `properties.miniblue` remains as-is for backward compatibility (image,
  powerState, containerName, custom-data status).
- Pass through every other property block the provider may send, rather than
  only the enumerated set: `diagnosticsProfile` (boot diagnostics),
  `priority`, `evictionPolicy`, `billingProfile`, `licenseType`,
  `securityProfile`, `additionalCapabilities`, `scheduledEventsProfile`,
  `availabilitySet`, `capacityReservation`, `proximityPlacementGroup`,
  `virtualMachineScaleSet`, `platformFaultDomain`, `host`, `hostGroup`,
  `applicationProfile`, `extensionsTimeBudget`, `placement`, and `zones`/
  `plan`/`extendedLocation` at the top level. Unknown blocks are preserved
  verbatim. Strip only write-only/generated fields: `osProfile.adminPassword`,
  any `protectedSettings` on extensions, and internal `_` fields.
- Support `?$expand=instanceView` and `?$expand=UserData`; the fields are
  always included anyway, so no special parsing is required.

### Additional VM endpoints

Add these alongside the existing VM routes:

- `GET /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/virtualMachines/{vm}/instanceView`
  → the same `instanceView` object (not wrapped in `properties`).
- `GET /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/virtualMachines/{vm}/vmSizes`
  → `{"value":[...]}` with a broad list of common sizes (`Standard_B1s`,
  `Standard_B2s`, `Standard_D2s_v3`, `Standard_D4s_v3`, `Standard_DS1_v2`,
  ...). The provider uses this only to decide whether it must deallocate before
  resizing; returning a superset is fine.
- `POST .../{vm}/deallocate` → stop the container and services and set the
  stored power state to `stopped` (or `deallocated`; the provider treats both
  as needing a start after an update). `start` already starts from either state.
- `start`, `powerOff`, `restart` and `deallocate` become metadata-only when
  Docker is unavailable: they update the stored power state and return `200`
  instead of `409 DockerUnavailable`. This is a deliberate change from today's
  behaviour so provider updates that power-cycle a VM still work in Docker-free
  CI. `runCommand`, logs, deploy/list/delete services and SSH still require
  Docker and keep returning `409`. The existing
  `TestVMRuntimeOperationsRequireDocker` must be updated to assert the new
  split.

---

## 4. VM PATCH and update semantics

Add `PATCH /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/virtualMachines/{vm}`.

- Refactor the current PUT-update branch and PATCH into one shared merge
  function.
- Merge rules:
  - `location`, `tags`, `zones`, `plan`: replace when present.
  - `identity`: replace when present; explicit JSON `null` removes it.
  - `properties`: recursive map merge. Arrays replace. `null` removes.
  - Preserve generated/internal fields (`id`, `vmId`, `_osDiskName`,
    `_identitySecret`, `_networkName`, `_provisionLog`, `miniblue` counters)
    unless the request explicitly changes the relevant public field.
- After merging:
  - Re-sync NIC links from `networkProfile.networkInterfaces`.
  - Re-sync disk attachments from `storageProfile.osDisk`/`dataDisks`.
  - If `osProfile.customData` changed, do not re-run it; append a provisioning
    event saying custom data changes only take effect on create.
  - Refresh power state and append an update event.
- PUT on an existing VM uses the same merge path for backward compatibility,
  but still returns `200`.
- PATCH on a missing VM returns `404`; PATCH does not create.

The provider's update path also calls `PowerOff`, `Deallocate` and `Start`
around some changes. Those endpoints must not depend on the VM's current state
in a way that errors when already stopped/started.

---

## 5. VM extensions

Add to `internal/services/vm` (`extensions.go`) and register under the existing
VM route:

```
.../virtualMachines/{vm}/extensions
  GET                         list
  /{extensionName}
    PUT, GET, PATCH, DELETE
```

Store key: `vmext:<sub>:<rg>:<vm>:<name>`.

Response:

```json
{
  "id": ".../virtualMachines/vm/extensions/ext",
  "name": "ext",
  "type": "Microsoft.Compute/virtualMachines/extensions",
  "location": "eastus",
  "tags": {},
  "properties": {
    "provisioningState": "Succeeded",
    "publisher": "Microsoft.Azure.Extensions",
    "type": "CustomScript",
    "typeHandlerVersion": "2.0",
    "autoUpgradeMinorVersion": true,
    "enableAutomaticUpgrade": false,
    "settings": {},
    "suppressFailures": false,
    "provisionAfterExtensions": [],
    "instanceView": {
      "name": "ext",
      "type": "CustomScript",
      "typeHandlerVersion": "2.0",
      "status": {
        "code": "ProvisioningState/succeeded",
        "level": "Info",
        "displayStatus": "Provisioning succeeded"
      }
    }
  }
}
```

Rules:

- Echo `publisher`, `type`, `typeHandlerVersion`, `autoUpgradeMinorVersion`,
  `enableAutomaticUpgrade`, `settings`, `suppressFailures`,
  `provisionAfterExtensions`, `protectedSettingsFromKeyVault`.
- Do not return `protectedSettings` (Azure hides it).
- `PATCH` merges settings/tags.
- `GET` requires the VM to exist (404 if not).
- VM delete cascades extensions; RG delete removes `vmext:` keys.
- Extensions are metadata-only. Do not execute CustomScript extension scripts;
  script execution is covered by `custom_data` below.

---

## 6. SSH public keys

New package `internal/services/sshkeys`:

```
/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/sshPublicKeys
  GET                         list in resource group
  /{keyName}
    PUT, GET, PATCH, DELETE
    POST /generateKeyPair
/subscriptions/{sub}/providers/Microsoft.Compute/sshPublicKeys
  GET                         list in subscription
```

Store key: `sshkey:<sub>:<rg>:<name>`.

Response:

```json
{
  "id": ".../Microsoft.Compute/sshPublicKeys/mykey",
  "name": "mykey",
  "type": "Microsoft.Compute/sshPublicKeys",
  "location": "eastus",
  "tags": {},
  "properties": {
    "publicKey": "ssh-rsa AAAA... miniblue",
    "provisioningState": "Succeeded"
  }
}
```

Rules:

- Echo a supplied `publicKey`.
- If no public key is supplied, generate a real 2048-bit RSA key at create
  time and return its OpenSSH public key. Use `golang.org/x/crypto/ssh` (already
  an indirect dependency) to marshal it; `go mod tidy` will promote it to a
  direct requirement.
- `POST /generateKeyPair` returns `{"publicKey":"ssh-rsa ...","privateKey":
  "-----BEGIN RSA PRIVATE KEY-----..."}`. It may generate a fresh pair or
  return the stored one; the provider only needs a valid public key.
- `PATCH` supports tags and `publicKey`.
- RG delete removes `sshkey:` keys.

---

## 7. Custom data execution

On VM create, after the container starts:

1. Read `properties.osProfile.customData` (base64). If it is empty, also check
   `properties.userData`; if that is empty, do nothing.
2. Try standard base64 and raw URL-safe base64. If both fail, treat the value
   as raw script text.
3. Trim a UTF-8 BOM and normalise CRLF to LF.
4. If the decoded text starts with `#cloud-config`, do not run it. Record
   `properties.miniblue.customData = {"executed": false, "reason":
   "cloud-config is not supported; scripts only"}` and a provisioning event.
5. Otherwise execute it inside the VM container with the existing Docker
   helper (`docker exec <container> sh -c <script>`). A shebang line is a
   no-op comment to `sh`; if the script explicitly requires bash, try
   `bash -c` when `sh -c` is not enough, but do not add a new shell framework.
6. Record
   ```
   properties.miniblue.customData = {
     "executed": true,
     "exitCode": 0,
     "output": "<truncated combined output, max 16 KiB>",
     "shell": "sh"
   }
   ```
   and a provisioning event. Creation still succeeds when the script exits
   non-zero; Azure does not fail VM provisioning for a failed cloud-init script.
7. Without Docker, record `{"executed": false, "reason": "docker unavailable"}`.

Do not execute custom data on update. The provider treats `custom_data` as
create-time input; re-running it would be surprising and differs from Azure.

---

## 8. VM cascade and resource group cleanup

- `resourcegroups.Delete` must remove the new prefixes:
  `nic:`, `disk:`, `sshkey:`, `vmext:`.
- `vm.CleanupVMsInRG` must also delete `vmext:` records for each VM and clear
  NIC links, in addition to removing containers/networks.
- VM delete removes `vmext:` children and clears NIC/disk links as described
  above.
- Disk delete is blocked while attached; RG delete bypasses that by removing
  all records.

---

## 9. VM gap tests

Add HTTP-level tests in `tests/` (following `vm_test.go` and `network_test.go`):

- `TestNetworkInterfaceLifecycle`: create VNet/subnet, public IP and NSG; create
  a NIC with a dynamic IP config linking subnet + public IP + NSG + LB backend
  pool; assert generated private IP, MAC, `internalDomainNameSuffix`,
  `virtualMachine` empty; static allocation honours the supplied IP; list;
  PATCH tags; delete; 404 after delete.
- `TestVMWithNICAndOSDisk`: create NIC, then VM referencing it; assert the VM
  response contains `networkProfile.networkInterfaces`, `instanceView` power
  state, `vmId`, and an OS disk with a generated name and managed-disk ID;
  assert the disk exists via `Microsoft.Compute/disks` with `managedBy` and
  `diskState: Attached`; assert NIC `virtualMachine.id`; delete the VM and
  assert the NIC link is cleared and the disk is unattached; delete the disk.
- `TestVMPatchAndDataDisks`: create VM + managed disk; PATCH tags, size and a
  data disk; assert GET reflects the merge and the disk is attached; PATCH
  removing the data disk detaches it; PATCH identity/tags; assert no drift in
  the returned shape. Also exercise `PUT` with the full model to mirror the
  data-disk-attachment provider path.
- `TestVMInstanceViewAndSizes`: `GET .../instanceView` has a
  `PowerState/running` status; `GET .../vmSizes` returns a non-empty list;
  `POST .../deallocate` then `POST .../start` transitions power state. This
  runs in stub mode now that power actions are metadata-only.
- Update `TestVMRuntimeOperationsRequireDocker`: `runCommand`, logs and service
  deploy still return `409 DockerUnavailable`; `start`, `powerOff`, `restart`
  and `deallocate` return `200` and change the recorded power state.
- `TestVMExtensionsAndSSHKeys`: extension PUT/GET/list/delete; extension
  protected settings are not returned; SSH key PUT/GET/list/delete and
  `generateKeyPair` returns a parseable public key. PUT must preserve a
  generated public key when the body omits it.
- `TestVMCustomDataStubMode`: with Docker disabled, custom data is recorded as
  skipped and does not fail creation.
- Integration (Docker) in `tests/integration/vm_docker_test.go` or a new file:
  create a VM with base64 `custom_data` that writes a file, then `runCommand`
  to read it back; a `#cloud-config` payload is skipped and recorded as such.

---

# Part 2: Front Door

## 10. Package and routes

New package `internal/services/frontdoor` with a single handler registering
both provider namespaces:

```
/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Cdn/profiles
  GET / PATCH? (list/create handled below)
  /{profile}
    PUT, GET, PATCH, DELETE
    /afdEndpoints
      GET
      /{endpoint}
        PUT, GET, PATCH, DELETE
        /routes
          GET
          /{route}
            PUT, GET, PATCH, DELETE
    /originGroups
      GET
      /{group}
        PUT, GET, PATCH, DELETE
        /origins
          GET
          /{origin}
            PUT, GET, PATCH, DELETE
    /ruleSets
      GET
      /{ruleSet}
        PUT, GET, PATCH, DELETE
        /rules
          GET
          /{rule}
            PUT, GET, PATCH, DELETE
    /customDomains
      GET
      /{domain}
        PUT, GET, PATCH, DELETE
    /secrets
      GET
      /{secret}
        PUT, GET, PATCH, DELETE
    /securityPolicies
      GET
      /{policy}
        PUT, GET, PATCH, DELETE
/subscriptions/{sub}/providers/Microsoft.Cdn/profiles
  GET                             list in subscription

/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/frontDoorWebApplicationFirewallPolicies
  GET                             list in resource group
  /{name}
    PUT, GET, PATCH, DELETE
/subscriptions/{sub}/providers/Microsoft.Network/frontDoorWebApplicationFirewallPolicies
  GET                             list in subscription
```

Register both `frontDoorWebApplicationFirewallPolicies` and
`FrontDoorWebApplicationFirewallPolicies` path spellings; ARM callers differ in
casing. Responses use `Microsoft.Network/FrontDoorWebApplicationFirewallPolicies`.

## 11. Shared nested-resource handler

Most of the CDN tree is plain CRUD. Implement one generic nested handler driven
by a `resourceSpec`:

```go
type resourceSpec struct {
    kind        string   // "endpoint", "originGroup", ...
    keyKind     string   // e.g. "endpoint" (the kind segment in the store key)
    segment     string   // e.g. "afdEndpoints"
    armType     string   // e.g. "Microsoft.Cdn/profiles/afdEndpoints"
    parents     []string // chi params needed to build the parent key, e.g. profileName
    computed    func(parent map[string]interface{}, input map[string]interface{},
                      existing map[string]interface{}) map[string]interface{}
    children    []string // key kinds removed on delete
}
```

The generic operations:

- Build the parent key from the URL params, fetch the parent, and 404 if it is
  missing.
- Build the resource ID as `<parent.id>/<segment>/<name>` (for origins:
  `<originGroup.id>/origins/<name>`; for rules:
  `<ruleSet.id>/rules/<name>`).
- Build the store key as
  `fd:<sub>:<rg>:<keyKind>:<profile>[:<parent names>]:<name>`.
- On PUT: merge the request body over an existing record (preserving generated
  fields), run `computed`, store, return `201`/`200`.
- On PATCH: recursive merge, run `computed` (which preserves generated values
  when already present), store, return `200`.
- On GET/list: return the stored record with `computed` applied so computed
  fields are always present even for old records.
- On DELETE: remove the record and its child prefixes, then return `202`.
- On list: scan the key prefix for the parent scope and return `{"value":[...]}`.
- All handlers set `etag: "W/\"miniblue\""` (or a stable per-record etag) and
  `tags: {}` when absent.

Key layout (all under subscription/resource-group/profile):

Subscription and resource group come immediately after `fd:`, so one
`DeleteByPrefix` can remove a resource group's entire Front Door footprint.

| Resource | Key |
|----------|-----|
| Profile | `fd:<sub>:<rg>:profile:<profile>` |
| Endpoint | `fd:<sub>:<rg>:endpoint:<profile>:<endpoint>` |
| Origin group | `fd:<sub>:<rg>:origingroup:<profile>:<group>` |
| Origin | `fd:<sub>:<rg>:origin:<profile>:<group>:<origin>` |
| Route | `fd:<sub>:<rg>:route:<profile>:<endpoint>:<route>` |
| Rule set | `fd:<sub>:<rg>:ruleset:<profile>:<ruleSet>` |
| Rule | `fd:<sub>:<rg>:rule:<profile>:<ruleSet>:<rule>` |
| Custom domain | `fd:<sub>:<rg>:customdomain:<profile>:<domain>` |
| Secret | `fd:<sub>:<rg>:secret:<profile>:<secret>` |
| Security policy | `fd:<sub>:<rg>:securitypolicy:<profile>:<policy>` |
| WAF policy | `fd:<sub>:<rg>:waf:<name>` |

Cascade rules:

- Delete profile → delete all `fd:<sub>:<rg>:<kind>:<profile>` prefixes
  (endpoint, route, origin group, origin, rule set, rule, custom domain,
  secret, security policy).
- Delete endpoint → delete its routes.
- Delete origin group → delete its origins.
- Delete rule set → delete its rules.
- Delete custom domain → remove its ID from every route's
  `properties.customDomains` array in the same profile, then delete the record.
- Delete secret → allowed; do not chase custom-domain TLS references.
- Delete WAF → only the WAF record.
- RG delete → `DeleteByPrefix("fd:" + sub + ":" + rg + ":")`, which covers all
  Front Door kinds for that resource group and nothing else.

## 12. Common computed properties

Every resource:

- `properties.provisioningState: "Succeeded"`.
- `properties.resourceState: "Active"`.
- `properties.deploymentStatus: "NotStarted"` where the resource has that field
  (endpoint, route, rule set, rule, custom domain, secret).
- `location`: echo input, else `"global"` for profile/endpoint/route/rule/
  rule set/custom domain/secret/security policy; WAF uses `"Global"`.

Profile:

- `properties.frontDoorId`: stable UUID generated at create. The provider
  exposes it as `resource_guid`.
- `properties.originResponseTimeoutSeconds`: input or `60`.
- Echo `sku.name`, `identity`, `tags`, `properties.logScrubbing`.

Endpoint:

- `properties.hostName`: generated once at create:
  `<lowercased name>-<6 lowercase hex>.z01.azurefd.net`. Store it in the record
  so it is stable across updates/GETs.
- `properties.enabledState`: input `"Enabled"`/`"Disabled"`, default
  `"Enabled"`.

Origin group:

- `healthProbeSettings`: echo input; defaults
  `{"probePath":"/","probeRequestType":"HEAD","probeProtocol":"Http",
  "probeIntervalInSeconds":30}`.
- `loadBalancingSettings`: echo input; defaults
  `{"sampleSize":4,"successfulSamplesRequired":3,
  "additionalLatencyInMilliseconds":0}`.
- `sessionAffinityState`: input or `"Disabled"`.
- `trafficRestorationTimeToHealedOrNewEndpointsInMinutes`: input or `10`.

Origin:

- Echo `hostName`, `originHostHeader`, `httpPort`, `httpsPort`, `priority`,
  `weight`, `enabledState`, `enforceCertificateNameCheck`,
  `sharedPrivateLinkResource`, `privateLinkAlias`.
- Defaults: `originHostHeader` = host name, `httpPort` = 80,
  `httpsPort` = 443, `priority` = 1, `weight` = 1000, `enabledState` =
  `"Enabled"`, `enforceCertificateNameCheck` = true.

Route:

- Echo `originGroup`, `ruleSets`, `customDomains`, `cacheConfiguration`,
  `originPath`, `patternsToMatch`, `supportedProtocols`,
  `forwardingProtocol`, `linkToDefaultDomain`, `httpsRedirect`,
  `enabledState`.
- Defaults: `supportedProtocols: ["Https"]`, `patternsToMatch: ["/*"]`,
  `forwardingProtocol: "MatchRequest"`, `linkToDefaultDomain: "Enabled"`,
  `httpsRedirect: "Enabled"`, `enabledState: "Enabled"`, `customDomains: []`,
  `ruleSets: []`.
- Set `properties.endpointName` from the parent endpoint.
- The provider reads `customDomains` to validate
  `azurerm_cdn_frontdoor_custom_domain_association`; preserve the array exactly
  as written, including order.

Rule set: common fields only.

Rule:

- Echo `order`, `conditions`, `actions`, `matchProcessingBehavior`.
  Defaults: `matchProcessingBehavior: "Continue"`, `conditions: []`,
  `actions: []`.
- Do not validate condition/action shapes; echo them.

Custom domain:

- Echo `hostName`, `azureDnsZone`, `tlsSettings`.
- Add:
  ```json
  "validationProperties": {
    "validationToken": "<32 lowercase hex, stable>",
    "expirationDate": "<created + 7 days, RFC3339>",
    "validationState": "Pending"
  },
  "domainValidationState": "Pending"
  ```
  Preserve the token/expiration on PATCH; generate only when absent.
- The provider reads `properties.validationProperties.validationToken` and
  `expirationDate`; it does not require validation to succeed for apply.

Secret:

- Echo `properties.parameters` (tagged union, e.g.
  `{"type":"CustomerCertificate","secretSource":{"id":"..."},
  "useLatestVersion":true}`).
- For `CustomerCertificate`, add `expirationDate` (future, stable) and
  `subjectAlternativeNames: []` when absent, so the provider's flatten does not
  return zero values for fields it expects.
- Echo `properties.provisioningState` and common fields.

Security policy:

- Echo `properties.parameters` (tagged union with
  `type: "WebApplicationFirewall"`, `wafPolicy: {"id": "..."}`,
  `associations: [...]`).
- For every association domain, set `isActive: true` when absent; preserve
  `patternsToMatch`.
- Add common computed fields.

WAF policy:

- `type`: `Microsoft.Network/FrontDoorWebApplicationFirewallPolicies`.
- `location`: `"Global"`.
- Echo `sku.name`, `tags`.
- `properties.policySettings`: merge input with defaults
  `{"enabledState":"Enabled","mode":"Prevention","requestBodyCheck":"Enabled"}`;
  preserve `redirectUrl`, `customBlockResponseStatusCode`,
  `customBlockResponseBody`, `javascriptChallengeExpirationInMinutes`,
  `captchaExpirationInMinutes`, `logScrubbing` when supplied.
- `properties.customRules`: echo input; default `{"rules":[]}`.
- `properties.managedRules`: echo input when supplied (Premium SKU).
- `properties.frontendEndpointLinks`: `[]`.
- `properties.provisioningState`: `"Succeeded"`.

## 13. Front Door tests

Add `tests/frontdoor_test.go`:

- `TestFrontDoorProfileAndEndpoint`: create profile (Standard SKU, tags), assert
  `frontDoorId`, `originResponseTimeoutSeconds`, SKU and tags; create endpoint,
  assert `hostName` matches `^<name>-[0-9a-f]{6}\.z01\.azurefd\.net$` and is
  stable across GETs; PATCH profile tags; list.
- `TestFrontDoorOriginTree`: origin group with health probe/load balancing;
  origin with host/ports/priority/weight; assert defaults and echo; list
  groups/origins; delete origin group cascades origins.
- `TestFrontDoorRouteAndRuleSet`: rule set + rule with conditions/actions and
  order; route linking origin group, rule set, custom domains, patterns and
  cache config; assert all fields round-trip; delete rule set cascades rules.
- `TestFrontDoorCustomDomainAndAssociation`: create custom domain with TLS and
  DNS zone; assert stable `validationToken` and `validationState`; create route
  with the domain ID; GET route shows `customDomains`; delete the custom domain
  and assert the route's array is cleaned. This mirrors the provider's
  client-side association behaviour.
- `TestFrontDoorSecretAndSecurityPolicy`: create a BYOC secret and assert
  `parameters` round-trip with `expirationDate`/`subjectAlternativeNames`;
  create a security policy referencing a WAF policy and custom domain/endpoint;
  assert `associations[].domains[].isActive` is true.
- `TestFrontDoorFirewallPolicy`: create a WAF policy with policy settings,
  custom rules and managed rules; assert defaults, tags and list; PATCH; delete.
- `TestFrontDoorCascade`: create a profile with every child type; delete the
  profile; assert every child 404s. Delete the WAF separately. Also verify RG
  cascade removes all `fd:` records.
- `TestFrontDoorUpdate`: PATCH tags/properties on profile, endpoint, origin
  group, origin, route, rule set, rule, custom domain, secret, security policy
  and WAF; assert GET reflects changes and generated values did not change.

---

# Part 3: Integration and documentation

## 14. Server registration

- Import the new packages in `internal/server/server.go`.
- Add named services `disks`, `sshkeys`, `frontdoor` to the filterable service
  list.
- `GET /health` should list the new service names and the correct count.
- `SERVICES=network` still enables NICs; `SERVICES=vm` still enables
  extensions. Filtering is explicit: enabling `vm` does not implicitly enable
  `network`, `disks` or `sshkeys`. A complete VM workflow needs
  `SERVICES=vm,network,disks,sshkeys` (plus the services the config actually
  uses). Document this dependency set rather than adding implicit expansion,
  which would change the meaning of every existing filter combination.

## 15. Documentation

- New `website/docs/services/front-door.md`: supported resource tree, API
  paths, an example OpenTofu/Terraform snippet, computed values, and
  limitations (no traffic routing, TLS termination, DNS validation or WAF
  enforcement; hostnames are generated; validation stays pending; secrets are
  metadata only).
- Update `website/docs/services/virtual-machines.md`:
  - NICs, managed disks, extensions, SSH public keys.
  - VM GET `instanceView`/`vmId`/disk details, PATCH updates, deallocate,
    `vmSizes`.
  - Custom data: base64 shell scripts only; `#cloud-config` is skipped;
    execution status is under `properties.miniblue.customData`.
  - Stub mode: power actions update metadata and succeed; `runCommand`, logs,
    services and SSH still return `409 DockerUnavailable`.
  - Remove the old "provisioning extensions, custom-data scripts ... are not
    emulated" limitation.
- Update `website/docs/services/virtual-networks.md` with NIC support (or add
  a short NIC section there).
- Update `website/docs/services/parity.md`:
  - NICs: Full create/get/list/delete/update.
  - Managed disks: Full create/get/list/delete/update; attachment tracking.
  - SSH public keys, VM extensions: Full.
  - Virtual Machines: Full with PATCH.
  - Front Door profile tree and WAF policy: Full for control-plane CRUD.
- Add `Front Door` to `website/mkdocs.yml`; update the README service table and
  count; remove completed items from `ROADMAP.md`'s "Next" table.
- Add representative OpenTofu fixtures under `examples/terraform/` (e.g.
  `examples/terraform/frontdoor/` and `examples/terraform/vm/`) that exercise
  the new resources. These are generic fixtures, not anyone's private config.
  Document how to run `tofu init/apply/plan/destroy` against miniblue.

## 16. Acceptance criteria

- `go test ./...` passes, including the new HTTP tests.
- `go vet ./...` passes (or the repo's configured lint).
- A representative OpenTofu/Terraform config can:
  1. `apply` a Linux VM with NIC, public IP, NSG, OS disk, data disk, SSH key,
     extension and custom data;
  2. `plan` immediately afterwards with no changes;
  3. update tags, VM size and data disks without replacement failures;
  4. `destroy` cleanly, with the provider's OS-disk DELETE succeeding;
  5. `apply` and `destroy` the full Front Door tree plus WAF policy;
  6. `plan` immediately after apply with no drift.
- If `tofu`/`terraform` is not available in the implementation environment, the
  HTTP-level tests are the required gate and the fixtures are still added for
  manual/CI use.
- Resource-group deletion removes all new records.
- Existing tests pass, with the one deliberate exception:
  `TestVMRuntimeOperationsRequireDocker` is updated for metadata-only power
  actions. All other existing service behaviour is unchanged.

## 17. Decisions left to the builder

- Exact internal helper names and whether the Front Door generic CRUD is
  implemented with closures or explicit per-resource wrappers.
- Whether to use `map[string]interface{}` for every response or small structs
  for the WAF policy.
- Exact set of common VM sizes in `vmSizes`.
- Whether `deallocate` stores `stopped` or `deallocated` in
  `properties.miniblue.powerState`; the provider treats both the same.
- Truncation length for custom-data output (design says 16 KiB).
- Whether to add unit tests inside the new packages in addition to HTTP tests.

## 18. Assumptions

- The existing Docker-backed VM container remains the compute environment; no
  new runtime backend is introduced.
- The provider may send API versions we do not model; the server remains
  version-lenient.
- Front Door hostnames and validation tokens are synthetic, but stable and
  shaped like the real values.
- The user's own OpenTofu configuration is not read or used as a fixture.
