package main

import (
	"path/filepath"
	"strings"
	"testing"

	"LyricsPipeline/internal/configtest"
	"LyricsPipeline/internal/infratest"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type infraMocks = infratest.Mocks

func withStackConfig(t *testing.T, values map[string]string) pulumi.RunOption {
	return func(info *pulumi.RunInfo) {
		info.Config = configtest.ValuesAt(t, "Pulumi.yaml")
		info.RootDirectory, _ = filepath.Abs(".")
		for key, value := range values {
			if !strings.Contains(key, ":") {
				key = "LyricsPipeline:" + key
			}
			switch strings.TrimPrefix(key, "LyricsPipeline:") {
			case "s3", "sqs":
				configtest.EditObject(t, info.Config, key, value)
			default:
				info.Config[key] = value
			}
		}
	}
}
func runDeployment(t *testing.T, values map[string]string) *infraMocks {
	t.Helper()
	m := &infraMocks{}
	if err := pulumi.RunErr(deploy, pulumi.WithMocks("LyricsPipeline", "test", m), withStackConfig(t, values)); err != nil {
		t.Fatal(err)
	}
	return m
}
func TestResourceGraphCompatibility(t *testing.T) {
	m := &infraMocks{}
	infratest.AssertGraph(t, "testdata/resource_graph.json", deploy, m, pulumi.WithMocks("LyricsPipeline", "test", m), withStackConfig(t, nil))
}
func TestPreviewUnknownOutputs(t *testing.T) {
	m := &infraMocks{PreviewUnknowns: true}
	if err := pulumi.RunErr(deploy, pulumi.WithMocks("LyricsPipeline", "test", m), withStackConfig(t, nil), func(info *pulumi.RunInfo) { info.DryRun = true }); err != nil {
		t.Fatal(err)
	}
	if len(m.AllResources) != 10 {
		t.Fatalf("incomplete graph: %d", len(m.AllResources))
	}
}

func TestPreviewDocumentRemainsUnknown(t *testing.T) {
	m := &infraMocks{PreviewUnknowns: true}
	if err := pulumi.RunErr(deploy, pulumi.WithMocks("LyricsPipeline", "test", m), withStackConfig(t, nil), func(info *pulumi.RunInfo) { info.DryRun = true }); err != nil {
		t.Fatal(err)
	}
	inputs, err := plugin.UnmarshalProperties(m.Resources["vocal-extractor-queue"].RegisterRPC.GetObject(), plugin.MarshalOptions{KeepUnknowns: true})
	if err != nil {
		t.Fatal(err)
	}
	if !inputs[resource.PropertyKey("redrivePolicy")].ContainsUnknowns() {
		t.Fatal("preview lost unknown document")
	}
}
