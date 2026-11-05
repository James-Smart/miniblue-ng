# Roadmap

This is the public roadmap for miniblue. It outlines what we are working on and what is planned.

Priorities shift based on community feedback. Open an issue or discussion if something here matters to you.

## Now (in progress)

- **Improve Terraform parity** - fill gaps in existing service responses so more `azurerm` resources work out of the box
- **Azure SDK compatibility** - test and fix responses for the official Azure SDKs (Go, Python, .NET, JS)
- **Better error messages** - return the exact error codes and formats that Azure returns so client libraries handle errors correctly
- **Document control plane vs data plane scope** - clarify what miniblue aims to emulate and where the boundary is

## Next (planned)

### New services

| Service | Provider | Priority | Notes |
|---------|----------|----------|-------|
| Azure Kubernetes Service (AKS) | `Microsoft.ContainerService` | High | ARM management only, no real cluster |
| App Service / Web Apps | `Microsoft.Web` | High | Extends existing Functions stub |
| Management Groups | `Microsoft.Management` | Medium | Hierarchy, subscription assignment |
| Azure Monitor | `Microsoft.Insights` | Medium | Metrics and diagnostic settings |
| Traffic Manager | `Microsoft.Network` | Medium | DNS-based routing profiles |
| Azure Front Door | `Microsoft.Cdn` | Medium | CDN and WAF policies |
| Private DNS Zones | `Microsoft.Network` | Medium | Extends existing DNS service |
| Azure Policy | `Microsoft.Authorization` | Medium | Policy definitions, assignments and evaluation on deployments |

### Platform improvements

- **Resource tags** - store and return tags on all ARM resources, support tag filtering on list operations
- **Bicep and ARM template deployment** - accept `Microsoft.Resources/deployments`, process ARM template JSON, support what-if operations. Enables `az deployment group create --template-file main.bicep` locally
- **Policy evaluation** - evaluate Azure Policy on resource create/update and return deny responses. Enables testing deployments against policy in regulated environments without a real Azure subscription
- **Persistent state across restarts** - improve file and PostgreSQL backends for production-like local environments
- **Webhooks and event delivery** - Event Grid subscriptions that call real HTTP endpoints
- **Service Bus subscriptions and dead-letter** - complete the messaging story
- **Key Vault keys and certificates** - extend beyond secrets
- **Cosmos DB query language** - basic SQL query support
- **Multi-tenancy** - support multiple tenants, subscriptions and management groups like real Azure. Allow creating/switching tenants and subscriptions for realistic multi-account testing

## Later (exploratory)

- Azure Active Directory / Entra ID (beyond mock tokens)
- Azure RBAC and role assignments
- Cosmos DB Mongo and Cassandra APIs
- Azure Batch
- Azure SignalR
- Logic Apps

## Scope

miniblue primarily targets the **ARM control plane** (resource CRUD via Terraform, Bicep, Azure CLI) with selective **data plane** support for common dev workflows (blob upload, queue send, secret get).

- Control plane (ARM) is the primary focus with broad service coverage
- Data plane is supported for high-value developer workflows
- Integration testing against real Azure is still recommended for data plane edge cases
- miniblue fills the gap between unit tests (mocks) and full integration tests (real Azure)

## Non-goals

These are explicitly out of scope:

- **Full Azure API compatibility** - miniblue aims for "good enough" responses that satisfy Terraform, SDKs and common workflows. Not 1:1 API parity
- **Production workloads** - miniblue is for local development and CI/CD testing
- **Authentication enforcement** - miniblue accepts any credentials by design
- **Multi-region replication** - all data is local, regions are cosmetic

## How to contribute

Pick anything from the roadmap and open a PR. The [CONTRIBUTING.md](CONTRIBUTING.md) has the full guide. Each service follows the same handler pattern. Look at any file in `internal/services/` for a template.

If you want to work on something, open an issue first so we can coordinate and avoid duplicate work.
