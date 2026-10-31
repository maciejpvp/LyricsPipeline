# Infrastructure compatibility fixture

`resource_graph.json` captures all 44 component/resource registrations from commit
`0adcb75701d44ddf8ca5ccc2b8d2b9af0bcae73f`, before the Go package refactor.
It uses the original defaults and Pulumi mocks with project `LyricsPipeline` and
stack `test`; no cloud resources, live identifiers or credentials are included.

The original policy-document mock returned `{}` for every invoke. For this
capture it was replaced with the typed IAM invoke serializer in
`internal/infratest/mocks.go`, so the fixture includes the actual original trust-policy
inputs. The original task and queue policies were already constructed directly
and are captured unchanged.

Resources are keyed by both type token and logical name. Keying only by name
would lose components because all eight components share the workload name.
This historical fixture is retained unchanged. Current per-layer compatibility tests use
`infra/{base,separation,transcription}/testdata/resource_graph.json`, captured from the
three layer entrypoints immediately before the deployment ownership refactor. Those fixtures
contain the resource snapshots **and stack exports**: 10 base, 35 separation and 29
transcription registrations. The base snapshot includes EventBridge enablement already present
before this refactor; consumers include their StackReference registrations.

Each layer's `TestResourceGraphCompatibility` compares exact type/name/parent identity,
inputs, dependency sets, `ignoreChanges` and exports using `internal/infratest`.
JSON container/redrive documents compare structurally; IAM comparisons additionally permit
semantically equivalent singleton/array forms and ordering. Absolute artifact paths are
normalized across checkout locations. Fixtures contain mock identifiers, not live credentials.
Mock graph equality is local regression evidence, not a cloud preview result.

Do not regenerate the fixture to make a refactor pass. Update it only when an
intentional infrastructure change has been reviewed and its effects understood.
