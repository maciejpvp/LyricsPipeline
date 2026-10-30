package main

import (
	"encoding/json"
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Only the provider calls used by this program are supported. An unexpected
// invoke must fail rather than silently return a convincing empty document.
func (m *infraMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	m.Lock()
	defer m.Unlock()
	m.calls = append(m.calls, args)
	region := m.providerRegion
	if region == "" {
		region = "eu-central-1"
	}
	switch args.Token {
	case "aws:index/getRegion:getRegion":
		return resource.PropertyMap{"region": resource.NewStringProperty(region)}, nil
	case "aws:index/getAvailabilityZone:getAvailabilityZone":
		zoneRegion := m.zoneRegion
		if zoneRegion == "" {
			zoneRegion = region
		}
		return resource.PropertyMap{"name": args.Args["name"], "region": resource.NewStringProperty(zoneRegion)}, nil
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
