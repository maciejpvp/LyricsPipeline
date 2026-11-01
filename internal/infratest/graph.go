package infratest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type ResourceSnapshot struct {
	Type, Name, Parent          string
	Inputs                      map[string]any
	Dependencies, IgnoreChanges []string
}
type Snapshot struct {
	Resources map[string]ResourceSnapshot `json:"resources"`
	Exports   map[string]interface{}      `json:"exports"`
}

// AssertGraph compares a fixture captured before refactoring. Ordering of
// dependencies and IAM statements is insignificant; other inputs compare exactly.
func AssertGraph(t *testing.T, path string, run pulumi.RunFunc, m *Mocks, opts ...pulumi.RunOption) {
	t.Helper()
	var want Snapshot
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	var exports map[string]interface{}
	wrapped := func(ctx *pulumi.Context) error {
		if err := run(ctx); err != nil {
			return err
		}
		pulumi.ToOutput(pulumi.Map(ctx.GetCurrentExportMap())).ApplyT(func(v map[string]interface{}) error { exports = v; return nil })
		return nil
	}
	if err := pulumi.RunErr(wrapped, opts...); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(jsonMap(t, exports), want.Exports) {
		t.Errorf("stack exports changed: got %v want %v", exports, want.Exports)
	}
	if len(m.AllResources) != len(want.Resources) {
		t.Errorf("resource count changed: got %d want %d", len(m.AllResources), len(want.Resources))
	}
	for key, old := range want.Resources {
		t.Run(key, func(t *testing.T) {
			current, ok := m.AllResources[key]
			if !ok {
				t.Fatal("resource identity removed")
			}
			if current.RegisterRPC.GetParent() != old.Parent {
				t.Error("parent changed")
			}
			if !reflect.DeepEqual(current.RegisterRPC.GetIgnoreChanges(), old.IgnoreChanges) {
				t.Error("ignoreChanges changed")
			}
			deps := append([]string(nil), current.RegisterRPC.GetDependencies()...)
			sort.Strings(deps)
			sort.Strings(old.Dependencies)
			if !reflect.DeepEqual(deps, old.Dependencies) {
				t.Errorf("dependencies changed: got %v want %v", deps, old.Dependencies)
			}
			got, expected := normalizedInputs(t, jsonMap(t, current.Inputs.Mappable())), normalizedInputs(t, old.Inputs)
			// Absolute artifact paths depend on checkout location, not resource configuration.
			for _, v := range []map[string]any{got, expected} {
				if code, ok := v["code"].(map[string]any); ok {
					if path, ok := code["path"].(string); ok {
						code["path"] = filepath.Base(path)
					}
				}
			}
			if !reflect.DeepEqual(got, expected) {
				t.Errorf("inputs changed:\n got %#v\nwant %#v", got, expected)
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
			parsed = NormalizePolicy(parsed)
		}
		inputs[field] = parsed
	}
	return inputs
}

func NormalizePolicy(value any) any {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			value[key] = NormalizePolicy(child)
		}
		return value
	case []any:
		for i, child := range value {
			value[i] = NormalizePolicy(child)
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

func jsonMap(t *testing.T, v map[string]interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]interface{}
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
