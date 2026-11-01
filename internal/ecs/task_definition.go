// Package ecs creates ECS task definitions, clusters and services from explicit inputs.
package ecs

import (
	"strconv"

	awsecs "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ecs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type TaskDefinitionArgs struct {
	Family               string
	Resources            Resources
	ExecutionRoleARN     pulumi.StringInput
	TaskRoleARN          pulumi.StringInput
	ContainerDefinitions pulumi.StringInput
	Tags                 pulumi.StringMap
}

func NewTaskDefinition(ctx *pulumi.Context, name string, args TaskDefinitionArgs, opts ...pulumi.ResourceOption) (*awsecs.TaskDefinition, error) {
	return awsecs.NewTaskDefinition(ctx, name, &awsecs.TaskDefinitionArgs{
		Family:                  pulumi.String(args.Family),
		Cpu:                     pulumi.String(strconv.Itoa(int(args.Resources.CPU))),
		Memory:                  pulumi.String(strconv.Itoa(int(args.Resources.Memory))),
		NetworkMode:             pulumi.String("awsvpc"),
		RequiresCompatibilities: pulumi.StringArray{pulumi.String("FARGATE")},
		ExecutionRoleArn:        args.ExecutionRoleARN,
		TaskRoleArn:             args.TaskRoleARN,
		ContainerDefinitions:    args.ContainerDefinitions,
		Tags:                    args.Tags,
	}, opts...)
}
