package main

import (
	"LyricsPipeline/internal/ecs"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type serviceComponent struct {
	pulumi.ResourceState
	*ecs.Service
}

func createService(ctx *pulumi.Context, cfg deploymentConfig, vpcID, subnetID pulumi.IDInput, taskARN pulumi.StringInput, ready []pulumi.Resource) (*serviceComponent, error) {
	c := &serviceComponent{}
	if err := ctx.RegisterComponentResource("lyrics:worker:Worker", cfg.Name, c); err != nil {
		return nil, err
	}
	parent := pulumi.Parent(c)
	group, err := ec2.NewSecurityGroup(ctx, cfg.Name+"-tasks", &ec2.SecurityGroupArgs{
		VpcId:       vpcID.ToIDOutput().ToStringPtrOutput(),
		Description: pulumi.String("Fargate worker egress"),
		Egress: ec2.SecurityGroupEgressArray{ec2.SecurityGroupEgressArgs{
			Protocol:   pulumi.String("-1"),
			FromPort:   pulumi.Int(0),
			ToPort:     pulumi.Int(0),
			CidrBlocks: pulumi.StringArray{pulumi.String(cfg.Network.Internet.String())},
		}},
		Tags: pulumi.ToStringMap(cfg.Tags),
	}, parent)
	if err != nil {
		return nil, err
	}
	service, err := ecs.NewService(ctx, cfg.Name, ecs.ServiceArgs{
		DesiredCount:      cfg.Scaling.MinTasks,
		CapacityProvider:  "FARGATE_SPOT",
		Subnets:           pulumi.StringArray{subnetID.ToIDOutput().ToStringOutput()},
		SecurityGroups:    pulumi.StringArray{group.ID()},
		TaskDefinitionARN: taskARN,
		Prerequisites:     ready,
		Tags:              pulumi.ToStringMap(cfg.Tags),
	}, parent)
	if err != nil {
		return nil, err
	}
	c.Service = service
	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{"clusterName": service.ClusterName, "serviceName": service.ServiceName}); err != nil {
		return nil, err
	}
	return c, nil
}
