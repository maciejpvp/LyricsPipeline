// Package infratest provides AWS provider mocks and resource graph comparisons.
// Application fixtures and deployment configuration belong to the calling tests.
package infratest

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Mocks struct {
	sync.Mutex
	Resources        map[string]pulumi.MockResourceArgs
	AllResources     map[string]pulumi.MockResourceArgs
	Calls            []pulumi.MockCallArgs
	ProviderRegion   string
	ZoneRegion       string
	PreviewUnknowns  bool
	ReferenceOutputs resource.PropertyMap
	BeforeResource   func(pulumi.MockResourceArgs)
}

func (m *Mocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	if m.BeforeResource != nil {
		m.BeforeResource(args)
	}
	m.Lock()
	defer m.Unlock()
	if m.Resources == nil {
		m.Resources = map[string]pulumi.MockResourceArgs{}
	}
	m.Resources[args.Name] = args
	if m.AllResources == nil {
		m.AllResources = map[string]pulumi.MockResourceArgs{}
	}
	m.AllResources[args.TypeToken+"::"+args.Name] = args
	outputs := args.Inputs.Copy()
	if args.TypeToken == "pulumi:pulumi:StackReference" {
		outputs["outputs"] = resource.NewObjectProperty(m.ReferenceOutputs)
		if m.PreviewUnknowns {
			unknowns := m.ReferenceOutputs.Copy()
			for key, value := range unknowns {
				unknowns[key] = resource.MakeComputed(value)
			}
			outputs["outputs"] = resource.NewObjectProperty(unknowns)
		}
		return args.Name, outputs, nil
	}
	if args.TypeToken == "aws:ec2/vpcEndpoint:VpcEndpoint" && args.Inputs["vpcEndpointType"].StringValue() == "Gateway" {
		outputs["prefixListId"] = resource.NewStringProperty("pl-0123456789abcdef0")
		if m.PreviewUnknowns {
			outputs["prefixListId"] = resource.MakeComputed(outputs["prefixListId"])
		}
	}
	for key, value := range map[resource.PropertyKey]string{
		"name": args.Name, "arn": "arn:aws:mock::123456789012:" + args.Name,
		"bucket": args.Name, "url": "https://sqs.example/" + args.Name,
		"repositoryUrl": "registry.example/" + args.Name,
	} {
		if _, ok := outputs[key]; !ok {
			outputs[key] = resource.NewStringProperty(value)
			if m.PreviewUnknowns && args.Custom {
				outputs[key] = resource.MakeComputed(outputs[key])
			}
		}
	}
	return args.Name + "-id", outputs, nil
}

// Only the provider Calls used by this program are supported. An unexpected
// invoke must fail rather than silently return a convincing empty document.
func (m *Mocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	m.Lock()
	defer m.Unlock()
	m.Calls = append(m.Calls, args)
	region := m.ProviderRegion
	if region == "" {
		region = "eu-central-1"
	}
	switch args.Token {
	case "aws:index/getCallerIdentity:getCallerIdentity":
		return resource.PropertyMap{"accountId": resource.NewStringProperty("123456789012"), "arn": resource.NewStringProperty("arn:aws:iam::123456789012:user/test"), "userId": resource.NewStringProperty("test")}, nil
	case "aws:index/getRegion:getRegion":
		return resource.PropertyMap{"region": resource.NewStringProperty(region)}, nil
	case "aws:index/getAvailabilityZone:getAvailabilityZone":
		ZoneRegion := m.ZoneRegion
		if ZoneRegion == "" {
			ZoneRegion = region
		}
		return resource.PropertyMap{"name": args.Args["name"], "region": resource.NewStringProperty(ZoneRegion)}, nil
	case "aws:iam/getPolicyDocument:getPolicyDocument":
		return mockPolicyDocument(args.Args)
	default:
		return nil, fmt.Errorf("unexpected provider invoke %q", args.Token)
	}
}

// This mock serializes the small IAM provider contract used by our tests.
// Real policy canonicalization and validation remain the provider's job.
func mockPolicyDocument(properties resource.PropertyMap) (resource.PropertyMap, error) {
	data, err := json.Marshal(properties.Mappable())
	if err != nil {
		return nil, err
	}
	var args iam.GetPolicyDocumentArgs
	if err := json.Unmarshal(data, &args); err != nil {
		return nil, err
	}
	type statement struct {
		Effect    string                         `json:"Effect"`
		Action    []string                       `json:"Action"`
		Resource  []string                       `json:"Resource,omitempty"`
		Principal map[string][]string            `json:"Principal,omitempty"`
		Condition map[string]map[string][]string `json:"Condition,omitempty"`
	}
	type document struct {
		Version   string      `json:"Version"`
		Statement []statement `json:"Statement"`
	}
	doc := document{}
	if args.Version != nil {
		doc.Version = *args.Version
	}
	for _, source := range args.Statements {
		item := statement{Action: source.Actions, Resource: source.Resources}
		if source.Effect != nil {
			item.Effect = *source.Effect
		}
		for _, principal := range source.Principals {
			if item.Principal == nil {
				item.Principal = map[string][]string{}
			}
			item.Principal[principal.Type] = append(item.Principal[principal.Type], principal.Identifiers...)
		}
		for _, condition := range source.Conditions {
			if item.Condition == nil {
				item.Condition = map[string]map[string][]string{}
			}
			if item.Condition[condition.Test] == nil {
				item.Condition[condition.Test] = map[string][]string{}
			}
			item.Condition[condition.Test][condition.Variable] = append(item.Condition[condition.Test][condition.Variable], condition.Values...)
		}
		doc.Statement = append(doc.Statement, item)
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return resource.PropertyMap{"json": resource.NewStringProperty(string(encoded))}, nil
}
