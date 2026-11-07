package main

import (
	"encoding/json"
	"os"
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
	infratest.AssertGraph(t, privateNetworkGraph(t), deploy, m, pulumi.WithMocks("LyricsPipeline-Separation", "test", m), withStackConfig(t, nil))
}

// Keep the historical fixture intact. Only these explicitly approved network
// changes may differ; identities, parents, IAM, scaling and exports still match.
func privateNetworkGraph(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/resource_graph.json")
	if err != nil {
		t.Fatal(err)
	}
	var graph infratest.Snapshot
	if err := json.Unmarshal(raw, &graph); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"aws:ec2/eip:Eip::vocal-extractor-nat-eip",
		"aws:ec2/natGateway:NatGateway::vocal-extractor-nat",
		"aws:ec2/route:Route::vocal-extractor-nat-route",
		"aws:ec2/route:Route::vocal-extractor-internet-route",
		"aws:ec2/routeTable:RouteTable::vocal-extractor-public-routes",
		"aws:ec2/routeTableAssociation:RouteTableAssociation::vocal-extractor-public-association",
		"aws:ec2/subnet:Subnet::vocal-extractor-public",
		"aws:ec2/internetGateway:InternetGateway::vocal-extractor-igw",
	} {
		if _, ok := graph.Resources[key]; !ok {
			t.Fatalf("historical resource missing: %s", key)
		}
		delete(graph.Resources, key)
	}
	graph.Resources["aws:ec2/subnet:Subnet::vocal-extractor-private"].Inputs["mapPublicIpOnLaunch"] = false
	groupKey := "aws:ec2/securityGroup:SecurityGroup::vocal-extractor-tasks"
	group := graph.Resources[groupKey]
	group.Inputs["ingress"] = []any{}
	group.Inputs["egress"] = workerEndpointEgress()
	group.Dependencies = append(group.Dependencies,
		"urn:pulumi:test::LyricsPipeline-Separation::lyrics:network:Network$aws:ec2/securityGroup:SecurityGroup::vocal-extractor-endpoints",
		"urn:pulumi:test::LyricsPipeline-Separation::lyrics:network:Network$aws:ec2/vpcEndpoint:VpcEndpoint::vocal-extractor-s3-endpoint",
	)
	graph.Resources[groupKey] = group

	serviceKey := "aws:ecs/service:Service::vocal-extractor-service"
	service := graph.Resources[serviceKey]
	var dependencies []string
	for _, urn := range service.Dependencies {
		switch resource.URN(urn).Name() {
		case "vocal-extractor-nat-route", "vocal-extractor-internet-route", "vocal-extractor-public-association":
			continue
		}
		dependencies = append(dependencies, urn)
	}
	for _, snapshot := range endpointSnapshots() {
		graph.Resources[snapshot.Type+"::"+snapshot.Name] = snapshot
		if snapshot.Type == "aws:ec2/vpcEndpoint:VpcEndpoint" || snapshot.Type == "aws:ec2/securityGroupRule:SecurityGroupRule" {
			parentType := resource.URN(snapshot.Parent).Type()
			dependencies = append(dependencies, "urn:pulumi:test::LyricsPipeline-Separation::"+string(parentType)+"$"+snapshot.Type+"::"+snapshot.Name)
		}
	}
	service.Dependencies = dependencies
	graph.Resources[serviceKey] = service
	updated, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "private-network-graph.json")
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestPreviewUnknownOutputs(t *testing.T) {
	m := &infraMocks{ReferenceOutputs: baseOutputs(), PreviewUnknowns: true}
	if err := pulumi.RunErr(deploy, pulumi.WithMocks("LyricsPipeline-Separation", "test", m), withStackConfig(t, nil), func(info *pulumi.RunInfo) { info.DryRun = true }); err != nil {
		t.Fatal(err)
	}
	if len(m.AllResources) != 34 {
		t.Fatalf("incomplete graph: %d", len(m.AllResources))
	}
	inputs, err := plugin.UnmarshalProperties(m.Resources["vocal-extractor-tasks"].RegisterRPC.GetObject(), plugin.MarshalOptions{KeepUnknowns: true})
	if err != nil {
		t.Fatal(err)
	}
	if !inputs["egress"].ContainsUnknowns() {
		t.Fatal("preview lost the unknown S3 prefix list in worker egress")
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
