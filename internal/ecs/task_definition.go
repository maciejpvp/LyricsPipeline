// Package ecs owns the worker task definition, logs, cluster and service.
// Task definitions and services retain their historical Pulumi component identities.
package ecs

import (
	"strconv"
	"time"

	"LyricsPipeline/internal/config"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	awsecs "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ecs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumix"
)

type TaskDefinitionArgs struct {
	Image            pulumi.StringInput
	Region           string
	Settings         config.Task
	Objects          config.ObjectLayout
	BucketName       pulumi.StringInput
	QueueURL         pulumi.StringInput
	ExecutionRoleARN pulumi.StringInput
	TaskRoleARN      pulumi.StringInput
	Tags             pulumi.StringMap
}

type TaskDefinition struct {
	pulumi.ResourceState
	ARN          pulumi.StringOutput
	LogGroupName pulumi.StringOutput
}

type taskDefinitionBuilder struct{ args TaskDefinitionArgs }

func NewTaskDefinition(ctx *pulumi.Context, name string, args TaskDefinitionArgs, opts ...pulumi.ResourceOption) (*TaskDefinition, error) {
	component := &TaskDefinition{}
	// The Go name changes; the token remains part of the existing resource identity.
	if err := ctx.RegisterComponentResource("lyrics:image:Image", name, component, opts...); err != nil {
		return nil, err
	}
	parent := pulumi.Parent(component)
	logs, err := cloudwatch.NewLogGroup(ctx, name+"-logs", &cloudwatch.LogGroupArgs{
		NamePrefix:      pulumi.String("/ecs/" + name + "-"),
		RetentionInDays: pulumi.Int(args.Settings.LogRetention / (24 * time.Hour)),
		Tags:            args.Tags,
	}, parent)
	if err != nil {
		return nil, err
	}
	b := taskDefinitionBuilder{args: args}
	containers := pulumi.JSONMarshal(pulumix.Apply4(
		args.Image.ToStringOutput(), args.BucketName.ToStringOutput(), args.QueueURL.ToStringOutput(), logs.Name, b.container,
	))
	definition, err := awsecs.NewTaskDefinition(ctx, name+"-task", &awsecs.TaskDefinitionArgs{
		Family:                  pulumi.String(name),
		Cpu:                     pulumi.String(strconv.Itoa(int(args.Settings.Resources.CPU))),
		Memory:                  pulumi.String(strconv.Itoa(int(args.Settings.Resources.Memory))),
		NetworkMode:             pulumi.String("awsvpc"),
		RequiresCompatibilities: pulumi.StringArray{pulumi.String("FARGATE")},
		ExecutionRoleArn:        args.ExecutionRoleARN,
		TaskRoleArn:             args.TaskRoleARN,
		ContainerDefinitions:    containers,
		Tags:                    args.Tags,
	}, parent)
	if err != nil {
		return nil, err
	}
	component.ARN, component.LogGroupName = definition.Arn, logs.Name
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{"logGroupName": component.LogGroupName, "taskDefinitionArn": component.ARN}); err != nil {
		return nil, err
	}
	return component, nil
}
