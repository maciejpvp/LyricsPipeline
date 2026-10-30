# Infrastructure Rules

These rules apply to all future Pulumi infrastructure changes in this repository.

## Keep this workload in one stack

This repository deliberately retains one Pulumi project and stack per environment.
The network, storage and worker currently share one owner and deployment lifecycle.
The Go refactor does not migrate Pulumi state or introduce new component parents.
Split stacks only as a separate, reviewed state migration when lifecycle or access
requirements actually diverge; use stack references at those boundaries.

## Name resource packages after AWS services

`main` runs `deployment.Run`. Resource packages use the AWS service name so the
provider is visible without opening the implementation. Cross-service orchestration
packages use their responsibility name. All implementation packages live under `internal`:

| Package | Responsibility |
| --- | --- |
| `config` | Public JSON parsing, typed settings, defaults and validation |
| `deployment` | Location checks, dependency wiring and stack exports |
| `vpc` | VPC, subnets, gateways and routing |
| `s3` | Private encrypted object storage |
| `sqs` | Processing queue, retry settings and DLQ |
| `ingestion` | Bucket-to-queue permission and object notifications |
| `iam` | Worker roles, trust and data permissions |
| `ecr` | Worker image repository |
| `ecs` | ECS task definition, container, logs, cluster, capacity providers and service |
| `autoscaling` | Scaling target, policies, metrics and alarms |

Use value argument structs with `New(ctx, name, args Args, opts...)`. The `ecs`
package has two explicit constructors: `NewTaskDefinition` with `TaskDefinitionArgs`
and `NewService` with `ServiceArgs`, in separate files. Alias AWS SDK imports when
the local package has the same name, for example `awss3` and `awsecs`.

Pass the specific typed outputs a consumer needs, plus explicit readiness dependencies,
instead of whole components from other packages. Use short methods on private
builders when a component has several creation steps. Small single-step
constructors do not need an extra builder. Keep provider declarations explicit;
do not introduce a generic resource factory or a catch-all utilities package.

## Use reusable components

Wrap related resources in a `pulumi.ComponentResource` instead of creating raw cloud resources directly in the stack entrypoint. Components should:

- expose typed argument and output structs;
- establish `pulumi.Parent(component)` for every child resource;
- apply project-wide security and naming defaults;
- register component outputs with `ctx.RegisterResourceOutputs`.

The reusable object storage implementation is in `internal/s3/bucket.go`. Keep
application-specific use cases, such as MP3 files, in the stack entrypoint or a
higher-level domain component rather than in the generic storage component.

### Component implementation requirements

Every reusable infrastructure group must follow the same shape:

- embed `pulumi.ResourceState` in the component output type;
- accept typed arguments and variadic `pulumi.ResourceOption` values;
- register the component before creating children;
- pass `pulumi.Parent(component)` to every child resource;
- register meaningful component outputs with `ctx.RegisterResourceOutputs`.

Keep logical names, component tokens and parents stable during code refactors.
`ecs.TaskDefinition` retains the `lyrics:image:Image` token; `ecs.Service` retains
`lyrics:worker:Worker`. `ingestion` is a wiring package, not an additional Pulumi
component: its policy and notification retain
the queue parent. Do not normalize historical tokens during unrelated cleanup.

Explicitly model readiness that cannot be inferred from IDs: notification after
queue policy, NAT after internet gateway, and ECS service after route associations,
internet/NAT routes and IAM policy attachments. The autoscaler owns desired count;
retain `IgnoreChanges(["desiredCount"])` on the ECS service.

## Serialize provider JSON structurally

Do not construct provider JSON using string interpolation or dynamic
`map[string]any` documents. Use concrete document structs and `pulumi.JSONMarshal`.
Use the provider's typed IAM policy-document builder for permission documents.
Resolve dynamic inputs using `pulumix.Apply` / `Apply2` / `Apply4` callbacks with
named, typed parameters. Do not use positional `pulumi.All` slices and assertions.

`pulumi.JSONMarshal` cannot serialize unknown outputs nested inside an ordinary
struct. Resolve the complete document to a top-level output before marshaling.
Resources must be registered outside apply callbacks so preview can see the graph.

Maps remain appropriate for genuinely dynamic keys such as tags and for required
SDK registration boundaries (`pulumi.Map`). Tests may inspect dynamic provider
property maps at the SDK boundary.

This rule applies to all Pulumi JSON fields, including IAM policies, SQS
redrive and queue policies, and ECS container definitions. JSON used by the
worker’s runtime protocol or by deployment configuration parsing is separate
application data and is not affected by this infrastructure rule.

## Decouple layers with stack references

When one layer needs an output from another layer, use `pulumi.StackReference`. Do not hardcode resource IDs, ARNs, or names from another stack.

```go
ref, err := pulumi.NewStackReference(ctx, "org/01-base-network/prod", nil)
if err != nil {
    return err
}
vpcID := ref.GetStringOutput(pulumi.String("vpcId"))
```

## Use strongly typed configuration

Keep the public flat `deployment` JSON keys and string CPU/memory values compatible.
`deploymentImage` continues to override `deployment.image`; an empty image retains
the provisioning-only `:bootstrap` reference. Parse this wire representation into
nested domain structs before registering resources. Reject unknown keys, malformed
or trailing JSON, unsupported Fargate CPU/memory pairs and out-of-range settings.
Errors must identify the setting that needs attention.

Use distinct `CPUUnits` and `MemoryMiB` types, `time.Duration` for time settings and
`netip.Prefix` for CIDRs. Convert units only at JSON/SDK boundaries. Keep defaults in
`config.Defaults()`; keep unrelated settings with equal values separate. The input
and output object prefixes have one owner (`config.ObjectLayout`) shared by IAM,
notifications and the container environment.

Resolve the current provider region and availability zone before resource creation.
Fail on location mismatches; do not silently create a new provider or move resources.
This needs read access to the provider region and EC2 availability-zone data source.
CloudWatch's one-minute metric evaluation permits an idle window of at most one day.
Queue visibility must be 1–43200 seconds, retries 2–1000, and the scale-out cooldown
must be a positive AWS 32-bit integer. Fargate size validation follows the
[AWS Linux task-size table](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/fargate-tasks-services.html).
The alarm-window bound follows [PutMetricAlarm](https://docs.aws.amazon.com/AmazonCloudWatch/latest/APIReference/API_PutMetricAlarm.html).

Never hardcode secrets. Use `config.RequireSecret` or encrypted Pulumi configuration values for credentials, tokens, and passwords.

## Resource naming and safety

- Prefer Pulumi-generated names or prefixes. Avoid hardcoding globally unique cloud resource names.
- Keep destructive options such as `ForceDestroy` disabled unless the requirement is explicit and documented. This stack explicitly enables force cleanup for its S3 bucket, ECR repository, and ECS service so teardown can remove stored objects, images, and service tasks.
- Default storage resources to private access.
- Add public-access blocking and ownership controls to S3 buckets unless a documented exception exists.
- Tag resources with their purpose and ownership where the provider supports tags.

## Delivery guardrails

- Run `pulumi preview` for every infrastructure pull request.
- Require review and successful checks before `pulumi up` on protected branches.
- Run formatting and tests for every Go infrastructure change.
- Preserve the single-stack exception above until a separate state migration is approved.
- Compare resource identities, parents, inputs and retained dependencies against
  `testdata/resource_graph.json`; do not regenerate that fixture to hide a difference.
- A refactor preview must not create, delete or replace resources. IAM JSON may be
  canonicalized by the provider, but effective permissions must remain unchanged.
- If stack access is unavailable, report that real preview remains unverified;
  mocked graph equality is not evidence of a successful cloud preview.
