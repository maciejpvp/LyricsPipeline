# Infrastructure Rules

These rules apply to Pulumi infrastructure changes in this repository.

## Deployment belongs to the layer

All projects share the root Go module and use the same layout:

| Directory / project | Owns |
| --- | --- |
| `infra/base` / `LyricsPipeline` | S3, separation queue/DLQ, queue permissions and the single bucket notification configuration |
| `infra/separation` / `LyricsPipeline-Separation` | Private worker VPC/endpoints, ECR, worker permissions, container definition, ECS and native scaling |
| `infra/transcription` / `LyricsPipeline-Transcription` | Transcribe workflow, EventBridge, Lambda environments/permissions, error queue and monitoring |
| `infra/contracts` | Versioned base outputs and their StackReference reader |

Every project owns `Pulumi.yaml`, stack configurations, `main.go`, `config.go` and
`deployment.go`. The local `deploy(ctx)` loads configuration, validates location and
dependencies, composes resources and exports outputs. Additional local files may
separate policies, container documents, workflow definitions or cross-service wiring.
Do not add a global deployment package or forwarding entrypoints.

Both consumers reference only the base and verify its contract version, account and
region. The layer passes the stack name explicitly to `contracts.Reference`; the contract
reader must not read project configuration. Transcription must deploy without separation,
ECS, VPC or NAT, and must not edit S3 notification configuration.

Moving the base source directory does not change its project identity or require a state
move. The earlier split from a monolithic state is a separate migration: identify the
owner, back up both states and follow the README before moving resources. Never infer
that AWS resources do not exist from an empty selected Pulumi state.

## Reusable packages have narrow responsibilities

`internal` packages provide service components or one specific infrastructure capability.
They must not import `infra`, application runtime packages, or project configuration models.
They accept their own typed parameters, concrete values, ARN/ID outputs and explicit
readiness dependencies. They must not take whole components from another service.

| Package | Responsibility |
| --- | --- |
| `config` | Strict JSON configuration decoding and generic boundary validation only |
| `lambdabuild` | Compile a caller-selected Go program and create a reproducible ZIP |
| `lambda` | Create a Lambda from a supplied artifact and AWS parameters |
| `iam` | Create roles and attach explicitly supplied policies |
| `ecs` | ECS definitions, clusters, capacity providers and services from explicit inputs |
| `vpc` | Private VPC, subnets, routing and generic service endpoints |
| `s3` | Private encrypted object storage |
| `sqs` | Queue, redrive configuration and DLQ |
| `ecr` | Image repository |
| `autoscaling` | Native queue-driven ECS scaling, metric math and alarms |

Application choices belong to layers: file-access policies, `MEDIA_BUCKET`, `QUEUE_URL`,
object prefixes, worker container layout, `TRANSCRIPTION_ACTION`, event filters and
Transcribe ASL. Cross-service composition, such as a bucket notification targeting a
queue, also belongs to the owning layer. Do not introduce a universal resource factory,
`utils` package or a transcription-specific infrastructure package under `internal`.

Use value argument structs and variadic `pulumi.ResourceOption` arguments. Alias AWS
imports where needed, for example `awss3`, `awsecs`. Larger components may use small
private builders. Small constructors do not need another builder or forwarding wrapper.

### Separate Lambda compilation from provisioning

`lambdabuild.Build(ctx, args)` accepts module directory, entry package, output directory
and Go architecture (`arm64` or `amd64`), and returns artifact path and architecture.
It must not import Pulumi/AWS or know any application, stack, environment or function name.
Archives contain an executable root `bootstrap` with deterministic ZIP metadata.

Do not add a repository-keyed/global application build cache. The layer builds once and
passes the artifact to each function; the Go toolchain handles compilation caching.
`lambda` accepts artifact, runtime, handler, AWS architecture, role, log group, resources
and environment. It does not compile programs, create business permissions or choose
application environment values. AWS's AMD64 architecture name is `x86_64`; callers make
that choice explicitly.

## Preserve resource identity and readiness

Existing tokens, logical names and parents are compatibility metadata owned by the layer.
Pass historical component tokens to reusable components rather than hardcoding application
tokens inside them. Layers may register compatibility parents when their children span
services. ECS task logs retain the `lyrics:image:Image` parent; service networking retains
`lyrics:worker:Worker`; worker IAM resources retain `lyrics:iam:WorkerIAM`. Bucket
notification and queue policy retain the queue component parent.

Grouped components embed `pulumi.ResourceState`, register before their children, set
`pulumi.Parent` for children and register meaningful outputs. Single-resource constructors
accept the caller's parent/options without adding another identity layer.

Model readiness that IDs alone do not imply: notification after queue policy,
and service after private routing associations, all service endpoints, endpoint ingress,
IAM attachments and cluster capacity providers. Keep the ECS service's
`IgnoreChanges(["desiredCount"])`: native Application Auto Scaling owns desired count.
Preserve Fargate Spot only, with no automatic On-Demand fallback.

Workers run in one private subnet without public IPs or a default internet route. Do not add
NAT, Elastic IPs, public subnets or an internet gateway. VPC DNS stays enabled. The layer
selects four interface services (ECR API, ECR registry, SQS and Logs); the VPC component
creates them with private DNS plus an S3 gateway endpoint on the private route table.
The gateway uses its default policy so ECR's layer bucket remains accessible; existing IAM
policies remain the authorization boundary. Do not change shared bucket/queue policies.

Workers explicitly manage empty ingress and allow TCP 443 only to the endpoint security
group and S3 prefix list. Endpoint ingress is owned exclusively by a standalone rule from
the worker group; do not also configure inline ingress on that group. This avoids a resource
dependency cycle. Endpoint egress is empty because replies are stateful. The shared ECS
service always disables public IP assignment.

Do not recreate resources as a refactoring strategy. Keep force-cleanup behavior unchanged:
this application explicitly enables it for S3, ECR and the ECS service. Storage stays private,
encrypted and protected against public access. Prefer generated physical names/prefixes and
tag resources where supported.

## Serialize provider documents structurally

Use concrete document structs with `pulumi.JSONMarshal`, and the provider's typed IAM
policy-document builder. Do not build JSON through string interpolation or dynamic
`map[string]any` documents. Resolve dynamic fields with typed `pulumix.Apply` callbacks.
Resolve the **complete** document to a top-level output before marshaling: unknown nested
outputs inside ordinary structs cannot be serialized correctly in preview. Register
resources outside apply callbacks so previews retain the full graph.

Maps are appropriate for dynamic tags and SDK registration boundaries (`pulumi.Map`).
Tests may inspect provider property maps and normalize semantically equivalent IAM JSON.

## Configuration is owned and validated locally

Every project's `Pulumi.yaml` contains its shared values, using native Pulumi `value:`
wrappers for objects. `Pulumi.<stack>.yaml` contains only deliberate overrides, stack
references and the published image. No application fallback values in Go and no production
YAML parsing: read Pulumi's resolved configuration through its API.

Objects use native whole-key replacement, not deep merging. Require every field, including
fields whose zero value is valid; reject missing, null and unknown fields. Wire structs,
application validation and unit conversion belong to the layer. Resource settings types
belong to their service. Use `time.Duration`, `netip.Prefix`, CPU units and MiB types where
appropriate. Protocol constants and AWS constraints remain code concerns.

Base groups: `s3`, `sqs`. Separation: `vpc`, `ecs`, `autoscaling`, scalar `image`.
Transcription: `transcription`. Each layer owns its name, region, tags and reference settings.
Reject legacy `deployment`/`deploymentImage` keys; the historical migration helper is used
only on the pre-split revision. Do not maintain runtime fallback precedence.

Validate configuration and actual provider location before registering resources. Keep
Fargate size, network layout and timing checks. Never hardcode secrets; use encrypted Pulumi
configuration and secret outputs when needed.

## Verification and delivery

- Capture current layer graphs before refactoring. Compare identities, parents, inputs,
  dependency sets, `ignoreChanges` and stack exports afterward.
- Keep layer fixtures in their own `testdata` directories. The original root 44-resource
  fixture remains historical evidence; do not regenerate fixtures to conceal changes.
- The private-endpoint migration preserves the historical 35-resource separation fixture and
  compares against an explicit test-side list of eight network removals and seven additions,
  restricted worker egress and connectivity dependencies; the resulting graph has 34 registrations.
- Keep application configuration/composition tests with their layer. Shared `configtest`
  and `infratest` helpers must not contain application defaults or deployment composition.
- Test the generic builder with multiple entry programs, both architectures, deterministic
  executable archives, edited sources and failed builds.
- Run Go tests, vet, appropriate race tests, script tests and previews of all projects.
  Report whether previews used real stack state or an isolated backend with synthetic outputs.
  Mock graph equality alone does not establish cloud behavior or migration safety.
- The source-directory refactor expected no resource deletion/replacement. The private-endpoint
  migration intentionally removes public/NAT resources while preserving the historical private
  subnet/routing and application identities. Follow the README drain procedure before any
  separately approved deployment; never delete connectivity while workers are still running.
  Review provider canonicalization separately from effective policy/container behavior.
- Require review before applying protected deployments. Never run `pulumi up` or mutate
  cloud state as part of this refactor's validation.
