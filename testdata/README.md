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

Intentional changes after the deployment refactor:

- 2026-11-01: the transcription fixture's two IAM role policies now allow
  `s3:ListBucket` on the shared bucket and `s3:PutObject` on the exact root-level
  `.write_access_check_file.temp` object used by Transcribe's output write check.
  Audio reads and raw-result writes retain their existing object scopes. Resource
  identities and other inputs are unchanged; `TestTranscribeS3Permissions` checks
  the complete S3 action/resource sets separately.
- 2026-11-01: at the operator's request, transcription source reads now cover
  the configured output prefix (`<bucket>/<outputPrefix>/*`) rather than only
  `vocals.wav`. This applies to Transcribe, the workflow and both Lambda roles.
  EventBridge filtering and runtime validation still require `vocals.wav`.
- 2026-11-01: Transcribe and workflow writes now cover
  `<bucket>/<outputPrefix>/*/transcription/*`, retaining the exact root-level
  write-check exception. The `transcribe:OutputKey` condition still requires
  `*/transcription/runs/*/raw.json`. Permission tests cover nested probes and
  reject writes to source audio, manifests and unrelated paths. The deployed
  workflow confirmed that Transcribe creates its probe alongside `raw.json`,
  at `*/transcription/runs/<run_id>/.write_access_check_file.temp`.
- 2026-11-01: superseding the write-check permissions above, transcription now
  uses service-managed output. The workflow omits output bucket/key parameters;
  IAM requires both to be absent. Transcribe and workflow S3 writes are removed.
  The finalization Lambda can read `lyrics-*` Transcribe job metadata and archives
  downloaded JSON at the existing raw-result path. These changes are covered by
  the workflow, IAM and runtime tests; no AWS deployment is implied by this fixture.
