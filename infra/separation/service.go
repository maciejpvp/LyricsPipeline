package main

import (
	"LyricsPipeline/internal/ecs"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const httpsPort = 443

type serviceComponent struct {
	pulumi.ResourceState
	*ecs.Service
}

type serviceArgs struct {
	Name                    string
	DesiredCount            int
	VPCID, SubnetID         pulumi.IDInput
	EndpointSecurityGroupID pulumi.IDInput
	S3PrefixListID          pulumi.StringInput
	TaskDefinitionARN       pulumi.StringInput
	Prerequisites           []pulumi.Resource
	Tags                    pulumi.StringMap
}

type serviceBuilder struct {
	ctx    *pulumi.Context
	args   serviceArgs
	parent pulumi.ResourceOption
}

type serviceNetwork struct {
	groupID pulumi.IDInput
	ingress pulumi.Resource
}

func createService(ctx *pulumi.Context, args serviceArgs) (*serviceComponent, error) {
	c := &serviceComponent{}
	if err := ctx.RegisterComponentResource("lyrics:worker:Worker", args.Name, c); err != nil {
		return nil, err
	}
	b := serviceBuilder{ctx: ctx, args: args, parent: pulumi.Parent(c)}
	group, err := b.createWorkerGroup()
	if err != nil {
		return nil, err
	}
	ingress, err := b.allowEndpointAccess(group.ID())
	if err != nil {
		return nil, err
	}
	c.Service, err = b.createECSService(serviceNetwork{groupID: group.ID(), ingress: ingress})
	if err != nil {
		return nil, err
	}
	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{"clusterName": c.ClusterName, "serviceName": c.ServiceName}); err != nil {
		return nil, err
	}
	return c, nil
}

func (b *serviceBuilder) createWorkerGroup() (*ec2.SecurityGroup, error) {
	return ec2.NewSecurityGroup(b.ctx, b.args.Name+"-tasks", &ec2.SecurityGroupArgs{
		VpcId: b.args.VPCID.ToIDOutput().ToStringPtrOutput(), Description: pulumi.String("Fargate worker egress"),
		Ingress: ec2.SecurityGroupIngressArray{},
		Egress: ec2.SecurityGroupEgressArray{
			ec2.SecurityGroupEgressArgs{
				Protocol: pulumi.String("tcp"), FromPort: pulumi.Int(httpsPort), ToPort: pulumi.Int(httpsPort),
				SecurityGroups: pulumi.StringArray{b.args.EndpointSecurityGroupID.ToIDOutput().ToStringOutput()},
			},
			ec2.SecurityGroupEgressArgs{
				Protocol: pulumi.String("tcp"), FromPort: pulumi.Int(httpsPort), ToPort: pulumi.Int(httpsPort),
				PrefixListIds: pulumi.StringArray{b.args.S3PrefixListID},
			},
		},
		Tags: b.args.Tags,
	}, b.parent)
}

func (b *serviceBuilder) allowEndpointAccess(workerGroupID pulumi.IDInput) (*ec2.SecurityGroupRule, error) {
	return ec2.NewSecurityGroupRule(b.ctx, b.args.Name+"-endpoints-ingress", &ec2.SecurityGroupRuleArgs{
		Type: pulumi.String("ingress"), Protocol: pulumi.String("tcp"),
		FromPort: pulumi.Int(httpsPort), ToPort: pulumi.Int(httpsPort),
		SecurityGroupId:       b.args.EndpointSecurityGroupID.ToIDOutput().ToStringOutput(),
		SourceSecurityGroupId: workerGroupID.ToIDOutput().ToStringPtrOutput(),
	}, b.parent)
}

func (b *serviceBuilder) createECSService(network serviceNetwork) (*ecs.Service, error) {
	ready := append([]pulumi.Resource{}, b.args.Prerequisites...)
	ready = append(ready, network.ingress)
	return ecs.NewService(b.ctx, b.args.Name, ecs.ServiceArgs{
		DesiredCount: b.args.DesiredCount, CapacityProvider: "FARGATE_SPOT",
		Subnets:           pulumi.StringArray{b.args.SubnetID.ToIDOutput().ToStringOutput()},
		SecurityGroups:    pulumi.StringArray{network.groupID.ToIDOutput().ToStringOutput()},
		TaskDefinitionARN: b.args.TaskDefinitionARN, Prerequisites: ready, Tags: b.args.Tags,
	}, b.parent)
}
