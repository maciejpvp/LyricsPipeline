# Infrastructure compatibility fixture

`resource_graph.json` captures all 44 component/resource registrations from commit
`0adcb75701d44ddf8ca5ccc2b8d2b9af0bcae73f`, before the Go package refactor.
It uses the original defaults and Pulumi mocks with project `LyricsPipeline` and
stack `test`; no cloud resources, live identifiers or credentials are included.

The original policy-document mock returned `{}` for every invoke. For this
capture it was replaced with the typed IAM invoke serializer in
`provider_mocks_test.go`, so the fixture includes the actual original trust-policy
inputs. The original task and queue policies were already constructed directly
and are captured unchanged.

Resources are keyed by both type token and logical name. Keying only by name
would lose components because all eight components share the workload name.
`TestResourceGraphCompatibility` checks exact type/name/parent identity, equivalent
inputs, original dependency edges and `ignoreChanges`. New readiness edges are
checked separately. JSON container and redrive documents compare structurally;
IAM comparisons also permit semantically equivalent singleton/array forms and
ordering. This fixture is local regression evidence, not a cloud preview result.

Do not regenerate the fixture to make a refactor pass. Update it only when an
intentional infrastructure change has been reviewed and its effects understood.
