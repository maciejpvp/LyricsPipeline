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

func baseOutputs() resource.PropertyMap {
	return resource.NewPropertyMapFromMap(map[string]interface{}{
		"contractVersion": 1, "region": "eu-central-1", "accountId": "123456789012", "mediaBucket": "media",
		"mediaBucketArn": "arn:aws:s3:::media", "inputPrefix": "uploads/audio", "outputPrefix": "results/stems",
		"queueUrl": "https://sqs.example/jobs", "queueArn": "arn:aws:sqs:eu-central-1:123456789012:jobs", "queueName": "jobs",
	})
}

func withStackConfig(t *testing.T, values map[string]string) pulumi.RunOption {
	return func(info *pulumi.RunInfo) {
		info.Config = configtest.ValuesAt(t, "Pulumi.yaml")
		info.Config["LyricsPipeline-Separation:baseStack"] = "org/LyricsPipeline/dev"
		info.RootDirectory, _ = filepath.Abs(".")
		for key, value := range values {
			if !strings.Contains(key, ":") {
				key = "LyricsPipeline-Separation:" + key
			}
			switch strings.TrimPrefix(key, "LyricsPipeline-Separation:") {
			case "autoscaling", "ecs", "vpc":
				configtest.EditObject(t, info.Config, key, value)
			default:
				info.Config[key] = value
			}
		}
	}
}
func runDeployment(t *testing.T, values map[string]string) *infraMocks {
	t.Helper()
	m := &infraMocks{ReferenceOutputs: baseOutputs()}
	if err := pulumi.RunErr(deploy, pulumi.WithMocks("LyricsPipeline-Separation", "test", m), withStackConfig(t, values)); err != nil {
		t.Fatal(err)
	}
	return m
}
func TestResourceGraphCompatibility(t *testing.T) {
	m := &infraMocks{ReferenceOutputs: baseOutputs()}
	infratest.AssertGraph(t, "testdata/resource_graph.json", deploy, m, pulumi.WithMocks("LyricsPipeline-Separation", "test", m), withStackConfig(t, nil))
}
func TestPreviewUnknownOutputs(t *testing.T) {
	m := &infraMocks{ReferenceOutputs: baseOutputs(), PreviewUnknowns: true}
	if err := pulumi.RunErr(deploy, pulumi.WithMocks("LyricsPipeline-Separation", "test", m), withStackConfig(t, nil), func(info *pulumi.RunInfo) { info.DryRun = true }); err != nil {
		t.Fatal(err)
	}
	if len(m.AllResources) != 35 {
		t.Fatalf("incomplete graph: %d", len(m.AllResources))
	}
}

func TestPreviewDocumentRemainsUnknown(t *testing.T) {
	m := &infraMocks{PreviewUnknowns: true, ReferenceOutputs: baseOutputs()}
	if err := pulumi.RunErr(deploy, pulumi.WithMocks("LyricsPipeline-Separation", "test", m), withStackConfig(t, nil), func(info *pulumi.RunInfo) { info.DryRun = true }); err != nil {
		t.Fatal(err)
	}
	inputs, err := plugin.UnmarshalProperties(m.Resources["vocal-extractor-task"].RegisterRPC.GetObject(), plugin.MarshalOptions{KeepUnknowns: true})
	if err != nil {
		t.Fatal(err)
	}
	if !inputs[resource.PropertyKey("containerDefinitions")].ContainsUnknowns() {
		t.Fatal("preview lost unknown document")
	}
}
