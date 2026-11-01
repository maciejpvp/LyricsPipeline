// Package configtest reads real Pulumi configuration for infrastructure tests.
// It deliberately contains no copied application defaults.
package configtest

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
)

func ValuesAt(t *testing.T, path string) map[string]string {
	t.Helper()
	project, err := workspace.LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for key, spec := range project.Config {
		value := spec.Value
		if value == nil {
			value = spec.Default
		}
		if value == nil {
			continue
		}
		if !strings.Contains(key, ":") {
			key = string(project.Name) + ":" + key
		}
		if text, ok := value.(string); ok {
			values[key] = text
			continue
		}
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		values[key] = string(data)
	}
	return values
}

// StackValuesAt follows Pulumi's whole-key override semantics, not a custom merge.
func StackValuesAt(t *testing.T, path, stackName string) map[string]string {
	t.Helper()
	values := ValuesAt(t, path)
	project, err := workspace.LoadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	stack, err := workspace.LoadProjectStack(nil, project, filepath.Join(filepath.Dir(path), "Pulumi."+stackName+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range stack.Config {
		raw, err := value.Value(nil)
		if err != nil {
			t.Fatal(err)
		}
		values[key.String()] = raw
	}
	return values
}

// EditObject models editing fields in the central YAML source. It produces a
// complete object; it does not pretend partial stack overrides are deep-merged.
func EditObject(t *testing.T, values map[string]string, key, patch string) {
	t.Helper()
	var changes map[string]json.RawMessage
	if err := json.Unmarshal([]byte(patch), &changes); err != nil || changes == nil {
		values[key] = patch
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(values[key]), &fields); err != nil {
		t.Fatal(err)
	}
	for name, value := range changes {
		fields[name] = value
	}
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	values[key] = string(data)
}
