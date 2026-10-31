// Package iam creates roles and attaches explicitly supplied policies.
package iam

import (
	awsiam "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Policy struct {
	Name     string
	Document pulumi.StringInput
}
type ManagedPolicy struct {
	Name string
	ARN  pulumi.StringInput
}
type Args struct {
	Trust           pulumi.StringInput
	Policies        []Policy
	ManagedPolicies []ManagedPolicy
	Tags            pulumi.StringMap
}
type Role struct {
	Name, ARN        pulumi.StringOutput
	PermissionsReady []pulumi.Resource
}

func NewRole(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Role, error) {
	role, err := awsiam.NewRole(ctx, name, &awsiam.RoleArgs{AssumeRolePolicy: args.Trust, Tags: args.Tags}, opts...)
	if err != nil {
		return nil, err
	}
	result := &Role{Name: role.Name, ARN: role.Arn}
	for _, p := range args.ManagedPolicies {
		resource, err := awsiam.NewRolePolicyAttachment(ctx, p.Name, &awsiam.RolePolicyAttachmentArgs{Role: role.Name, PolicyArn: p.ARN}, opts...)
		if err != nil {
			return nil, err
		}
		result.PermissionsReady = append(result.PermissionsReady, resource)
	}
	for _, p := range args.Policies {
		resource, err := AttachPolicy(ctx, p.Name, role.Name, p.Document, opts...)
		if err != nil {
			return nil, err
		}
		result.PermissionsReady = append(result.PermissionsReady, resource)
	}
	return result, nil
}
func AttachPolicy(ctx *pulumi.Context, name string, role, policy pulumi.StringInput, opts ...pulumi.ResourceOption) (*awsiam.RolePolicy, error) {
	return awsiam.NewRolePolicy(ctx, name, &awsiam.RolePolicyArgs{Role: role, Policy: policy}, opts...)
}
