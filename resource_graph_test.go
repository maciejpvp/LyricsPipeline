package main

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"LyricsPipeline/internal/deployment"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type resourceSnapshot struct {
	Type          string
	Name          string
	Parent        string
	Inputs        map[string]any
	Dependencies  []string
	IgnoreChanges []string
}

// The fixture was captured from the pre-refactor program, with a policy-document
// mock that serializes the real IAM invoke inputs (rather than returning "{}").
func TestResourceGraphCompatibility(t *testing.T) {
	data, err := os.ReadFile("testdata/resource_graph.json")
	if err != nil {
		t.Fatal(err)
	}
	var before map[string]resourceSnapshot
	if err := json.Unmarshal(data, &before); err != nil {
		t.Fatal(err)
	}
	mocks := runDeployment(t, nil)
	if len(mocks.allResources) != len(before) {
		t.Fatalf("resource count changed: %d -> %d", len(before), len(mocks.allResources))
	}
	for key, old := range before {
		t.Run(key, func(t *testing.T) {
			current, ok := mocks.allResources[key]
			if !ok {
				t.Fatalf("resource identity removed: %s", key)
			}
			if current.RegisterRPC.GetParent() != old.Parent {
				t.Errorf("parent changed: %s -> %s", old.Parent, current.RegisterRPC.GetParent())
			}
			if !reflect.DeepEqual(current.RegisterRPC.GetIgnoreChanges(), old.IgnoreChanges) {
				t.Error("ignoreChanges changed")
			}
			for _, dependency := range old.Dependencies {
				if !slices.Contains(current.RegisterRPC.GetDependencies(), dependency) {
					t.Errorf("dependency removed: %s", dependency)
				}
			}
			got := normalizedInputs(t, current.Inputs.Mappable())
			want := normalizedInputs(t, old.Inputs)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("resource inputs changed\ngot:  %#v\nwant: %#v", got, want)
			}
		})
	}
}

// AWS policy JSON permits singleton strings or arrays, and treats statement,
// action and resource ordering as insignificant. Other inputs compare exactly.
func normalizedInputs(t *testing.T, inputs map[string]any) map[string]any {
	t.Helper()
	for _, field := range []string{"assumeRolePolicy", "policy", "redrivePolicy", "containerDefinitions"} {
		raw, ok := inputs[field].(string)
		if !ok {
			continue
		}
		var parsed any
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			t.Fatalf("invalid %s: %v", field, err)
		}
		if field == "policy" || field == "assumeRolePolicy" {
			parsed = normalizePolicy(parsed)
		}
		inputs[field] = parsed
	}
	return inputs
}

func normalizePolicy(value any) any {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			value[key] = normalizePolicy(child)
		}
		return value
	case []any:
		for i, child := range value {
			value[i] = normalizePolicy(child)
		}
		if len(value) == 1 {
			return value[0]
		}
		slices.SortFunc(value, func(a, b any) int {
			left, _ := json.Marshal(a)
			right, _ := json.Marshal(b)
			return strings.Compare(string(left), string(right))
		})
		return value
	default:
		return value
	}
}

func runDeployment(t *testing.T, values map[string]string) *infraMocks {
	t.Helper()
	mocks := &infraMocks{}
	if err := pulumi.RunErr(deployment.Run, pulumi.WithMocks("LyricsPipeline", "test", mocks), withStackConfig(t, values)); err != nil {
		t.Fatal(err)
	}
	return mocks
}

func TestCreationReadiness(t *testing.T) {
	mocks := runDeployment(t, nil)
	for _, edge := range []struct{ child, parent string }{
		{"notification", "queue-policy"}, {"nat", "igw"}, {"service", "capacity-providers"},
		{"service", "internet-route"}, {"service", "public-association"}, {"service", "private-association"}, {"service", "nat-route"},
		{"service", "execution-policy"}, {"service", "task-policy"},
	} {
		t.Run(edge.child+"_after_"+edge.parent, func(t *testing.T) {
			current := mocks.resources["vocal-extractor-"+edge.child]
			for _, dependency := range current.RegisterRPC.GetDependencies() {
				if resource.URN(dependency).Name() == "vocal-extractor-"+edge.parent {
					return
				}
			}
			t.Fatalf("missing dependency: %s -> %s", edge.child, edge.parent)
		})
	}
}

func TestValidationBeforeRegistration(t *testing.T) {
	for _, tc := range []struct{ name, key, raw, region, zoneRegion, want string }{
		{name: "invalid configuration", key: "ecs", raw: `{"cpuUnits":"garbage"}`, want: "cpuUnits"},
		{name: "unknown setting", key: "autoscaling", raw: `{"maxTask":2}`, want: "maxTask"},
		{name: "provider mismatch", region: "us-east-1", want: "aws:region"},
		{name: "zone mismatch", zoneRegion: "us-east-1", want: "vpc.availabilityZone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mocks := &infraMocks{providerRegion: tc.region, zoneRegion: tc.zoneRegion}
			err := pulumi.RunErr(deployment.Run, pulumi.WithMocks("LyricsPipeline", "test", mocks), withStackConfig(t, map[string]string{tc.key: tc.raw}))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s, got %v", tc.want, err)
			}
			if len(mocks.allResources) != 0 {
				t.Fatalf("registered %d resources before validation completed", len(mocks.allResources))
			}
		})
	}
}

func TestUnknownProviderInvokeFails(t *testing.T) {
	if _, err := (&infraMocks{}).Call(pulumi.MockCallArgs{Token: "unknown"}); err == nil {
		t.Fatal("unexpected invoke accepted")
	}
}

// Preview must register the complete graph even while provider outputs are unknown.
// In particular, container/redrive JSON must remain a top-level unknown value.
func TestPreviewWithUnknownOutputs(t *testing.T) {
	data, err := os.ReadFile("testdata/resource_graph.json")
	if err != nil {
		t.Fatal(err)
	}
	var before map[string]resourceSnapshot
	if err := json.Unmarshal(data, &before); err != nil {
		t.Fatal(err)
	}
	mocks := &infraMocks{previewUnknowns: true}
	if err := pulumi.RunErr(deployment.Run, pulumi.WithMocks("LyricsPipeline", "test", mocks), withStackConfig(t, nil), func(info *pulumi.RunInfo) { info.DryRun = true }); err != nil {
		t.Fatal(err)
	}
	if len(mocks.allResources) != len(before) {
		t.Fatalf("preview registered %d of %d resource identities", len(mocks.allResources), len(before))
	}
	for key, old := range before {
		current, ok := mocks.allResources[key]
		if !ok || current.RegisterRPC.GetParent() != old.Parent {
			t.Errorf("preview lost identity/parent: %s", key)
		}
	}
	for _, field := range []struct{ resource, property string }{{"task", "containerDefinitions"}, {"queue", "redrivePolicy"}} {
		// MockResourceArgs.Inputs drops unknowns; decode the actual registration RPC.
		inputs, err := plugin.UnmarshalProperties(mocks.resources["vocal-extractor-"+field.resource].RegisterRPC.GetObject(), plugin.MarshalOptions{KeepUnknowns: true})
		if err != nil {
			t.Fatal(err)
		}
		if !inputs[resource.PropertyKey(field.property)].ContainsUnknowns() {
			t.Errorf("%s.%s should remain unknown in preview", field.resource, field.property)
		}
	}
}
