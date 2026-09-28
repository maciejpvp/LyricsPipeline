# Infrastructure Rules

These rules apply to all future Pulumi infrastructure changes in this repository.

## Organize by lifecycle layer

Do not put networking, shared services, IAM, and application resources into one large Pulumi program. Separate resources by how often they change and who is allowed to deploy them. A recommended layout is:

```text
infrastructure/
├── 01-base-network/       # VPC, subnets, gateways, DNS
├── 02-shared-services/    # EKS, RDS, Redis, S3 buckets
└── 03-apps/               # API Gateway, Lambda, application services
```

Each layer should have its own `Pulumi.yaml`, stack configuration, and entrypoint. This limits blast radius, keeps previews fast, and allows access to be granted per lifecycle layer.

## Use reusable components

Wrap related resources in a `pulumi.ComponentResource` instead of creating raw cloud resources directly in the stack entrypoint. Components should:

- expose typed argument and output structs;
- establish `pulumi.Parent(component)` for every child resource;
- apply project-wide security and naming defaults;
- register component outputs with `ctx.RegisterResourceOutputs`.

The reusable object storage implementation is in `components/storage.go`. Keep
application-specific use cases, such as MP3 files, in the stack entrypoint or a
higher-level domain component rather than in the generic storage component.

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

Load and validate Pulumi configuration in one typed configuration struct. Keep `config.Get`, defaults, validation, and environment-specific behavior out of individual resource definitions.

Never hardcode secrets. Use `config.RequireSecret` or encrypted Pulumi configuration values for credentials, tokens, and passwords.

## Resource naming and safety

- Prefer Pulumi-generated names or prefixes. Avoid hardcoding globally unique cloud resource names.
- Keep destructive options such as `ForceDestroy` disabled unless the requirement is explicit and documented.
- Default storage resources to private access.
- Add public-access blocking and ownership controls to S3 buckets unless a documented exception exists.
- Tag resources with their purpose and ownership where the provider supports tags.

## Delivery guardrails

- Run `pulumi preview` for every infrastructure pull request.
- Require review and successful checks before `pulumi up` on protected branches.
- Run formatting and tests for every Go infrastructure change.
- Keep unrelated lifecycle layers in separate stacks to reduce accidental changes.
