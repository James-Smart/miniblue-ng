# API Parity Matrix

What's implemented, stubbed, and unsupported in miniblue compared to real Azure APIs.

## Legend

- **Full**: API works and returns realistic responses
- **Stub**: API accepts requests and returns valid responses but no real backend processing
- **None**: API not implemented (returns 404)

## ARM Management APIs

| Service | Create | Get | List | Delete | Update | Notes |
|---------|--------|-----|------|--------|--------|-------|
| Resource Groups | Full | Full | Full | Full | Full (PATCH) | HEAD for existence check |
| Virtual Networks | Full | Full | Full | Full | - | Subnets included |
| Subnets | Full | Full | Full | Full | - | Cascade delete with VNet |
| DNS Zones | Full | Full | Full | Full | - | Auto-creates SOA/NS records |
| DNS Records | Full | Full | - | Full | - | A, CNAME, MX, TXT, SOA, NS |
| Container Registry | Full | Full | Full | Full | - | Name availability check, replications |
| Event Grid Topics | Full | Full | Full | Full | - | Event publishing on data plane |
| Azure Functions | Stub | Stub | Stub | Stub | - | No function execution |
| DB for PostgreSQL | Full | Full | Full | Full | - | Real DB creation via POSTGRES_URL |
| DB for MySQL | Stub | Stub | Stub | Stub | - | ARM only, no real MySQL |
| Azure SQL Database | Stub | Stub | Stub | Stub | - | ARM only, no real SQL Server |
| Azure Cache for Redis | Full | Full | Full | Full | - | Real connectivity check via REDIS_URL |
| Container Instances | Full | Full | Full | Full | - | Real Docker containers when available |
| Kubernetes Service (AKS) | Full | Full | Full | Full | - | Real k3s clusters with `AKS_BACKEND=k3s`; agent pool GET only |
| Virtual Machines | Full | Full | Full | Full | Full (PATCH) | Real Docker containers when available; NIC/disk linkage, instanceView, vmSizes, extensions, SSH keys, custom data |
| VM Extensions | Full | Full | Full | Full | Full (PATCH) | Metadata only; protected settings hidden, instanceView synthesized |
| Managed Disks | Full | Full | Full | Full | Full (PATCH) | Attachment tracking, OS/data disk lifecycle, delete blocked while attached |
| SSH Public Keys | Full | Full | Full | Full | Full (PATCH) | Generated RSA key pairs, `generateKeyPair` action |
| Network Interfaces | Full | Full | Full | Full | Full (PATCH) | Dynamic private IP allocation, VM link tracking |
| Public IP Addresses | Full | Full | Full | Full | - | Static/dynamic allocation, auto-generated IPs |
| Network Security Groups | Full | Full | Full | Full | - | Security rules, default rules, cascade delete |
| Load Balancer | Full | Full | Full | Full | - | Frontends, backends, rules, probes, NAT |
| Application Gateway | Full | Full | Full | Full | - | SKU, frontends, backends, listeners, rules, probes, WAF |
| Front Door | Full | Full | Full | Full | Full (PATCH) | Profile tree: endpoints/routes, origin groups/origins, rule sets/rules, custom domains, secrets and security policies; metadata only |
| Front Door WAF Policy | Full | Full | Full | Full | Full (PATCH) | Resource-group scoped `Microsoft.Network` policy; no request inspection |
| Storage Accounts | Full | Full | Full | Full | - | Shared key auth, blob/queue/table/file endpoints |
| Subscriptions | Full | Full | - | - | - | Mock subscription |
| Tenants | - | - | Full | - | - | Mock tenant |
| Providers | Full | Full | Full | - | - | Registration always succeeds |

## Data Plane APIs

| Service | Operations | Status | Notes |
|---------|-----------|--------|-------|
| Blob Storage | Create container, upload, download, list, delete | Full | Content-length tracking |
| Table Storage | Create table, upsert, get, query, delete entity | Full | Partition/row key support |
| Queue Storage | Create queue, send, receive, clear, delete | Full | Dequeue count tracking |
| Key Vault | Set, get, list, delete secrets | Full | Keys and certificates not supported |
| Cosmos DB | Create, get, replace, delete, list documents | Full | SQL API only, no query language |
| Service Bus | Create queue/topic, send, receive, delete | Full | No subscriptions, no dead-letter |
| App Configuration | Set, get, list, delete key-values | Full | No labels, no feature flags |
| Event Grid | Publish events | Full | No event subscriptions |
| Container Registry | List manifests, tags | Stub | Docker v2 API stubs only |

## Auth and Infrastructure

| Endpoint | Status | Notes |
|----------|--------|-------|
| OAuth2 token (v1 + v2) | Full | Returns valid JWT with oid/tid claims |
| OpenID configuration | Full | OIDC discovery for MSAL |
| Instance discovery | Full | Authority validation (internal) |
| Managed Identity (IMDS) | Full | Token endpoint for workload identity |
| Cloud metadata | Full | Terraform provider metadata |
| /health | Full | 33 services listed |
| /metrics | Full | Uptime, request count, error rate |

## Not Implemented

These Azure services have no emulation:

- Azure Kubernetes Service (AKS)
- Azure App Service
- Azure Monitor / Log Analytics
- Azure Active Directory (beyond mock tokens)
- Azure Policy
- Azure RBAC
- Azure Key Vault (keys, certificates)
- Azure Cosmos DB (Mongo, Cassandra, Gremlin APIs)
- Azure Service Bus (subscriptions, dead-letter)
