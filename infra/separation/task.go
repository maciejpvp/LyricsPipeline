package main

import (
	"time"

	"LyricsPipeline/infra/contracts"
	"LyricsPipeline/internal/ecs"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumix"
)

type taskComponent struct {
	pulumi.ResourceState
	ARN pulumi.StringOutput
}
type taskBuilder struct {
	settings Task
	region   string
}

type objectPrefixes struct{ Input, Output string }

func createTask(ctx *pulumi.Context, cfg deploymentConfig, base contracts.Base, image pulumi.StringInput, roles *workerIdentity) (*taskComponent, error) {
	c := &taskComponent{}
	if err := ctx.RegisterComponentResource("lyrics:image:Image", cfg.Name, c); err != nil {
		return nil, err
	}
	parent := pulumi.Parent(c)
	logs, err := cloudwatch.NewLogGroup(ctx, cfg.Name+"-logs", &cloudwatch.LogGroupArgs{
		NamePrefix:      pulumi.String("/ecs/" + cfg.Name + "-"),
		RetentionInDays: pulumi.Int(cfg.Task.LogRetention / (24 * time.Hour)),
		Tags:            pulumi.ToStringMap(cfg.Tags),
	}, parent)
	if err != nil {
		return nil, err
	}
	builder := taskBuilder{settings: cfg.Task, region: cfg.Region}
	containers := pulumix.Apply4(image.ToStringOutput(), base.BucketName, base.QueueURL, logs.Name, builder.container)
	prefixes := pulumix.Apply2(base.Objects.Input.ToStringOutput(), base.Objects.Output.ToStringOutput(), func(input, output string) objectPrefixes {
		return objectPrefixes{Input: input, Output: output}
	})
	definitions := pulumi.JSONMarshal(pulumix.Apply2(containers, prefixes, func(containers []containerDefinition, prefixes objectPrefixes) []containerDefinition {
		containers[0].Environment = append(containers[0].Environment,
			environmentVariable{Name: "INPUT_PREFIX", Value: prefixes.Input},
			environmentVariable{Name: "OUTPUT_PREFIX", Value: prefixes.Output},
		)
		return containers
	}))
	definition, err := ecs.NewTaskDefinition(ctx, cfg.Name+"-task", ecs.TaskDefinitionArgs{
		Family:               cfg.Name,
		Resources:            cfg.Task.Resources,
		ExecutionRoleARN:     roles.ExecutionRoleARN,
		TaskRoleARN:          roles.TaskRoleARN,
		Tags:                 pulumi.ToStringMap(cfg.Tags),
		ContainerDefinitions: definitions,
	}, parent)
	if err != nil {
		return nil, err
	}
	c.ARN = definition.Arn
	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{"logGroupName": logs.Name, "taskDefinitionArn": definition.Arn}); err != nil {
		return nil, err
	}
	return c, nil
}
