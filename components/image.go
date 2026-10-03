package components

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ecs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Image struct {
	pulumi.ResourceState
	LogGroup       *cloudwatch.LogGroup
	TaskDefinition *ecs.TaskDefinition
}

func NewImage(ctx *pulumi.Context, name string, image pulumi.StringInput, region, cpu, memory string, storage *Storage, queue *Queue, roles *WorkerIAM, tags pulumi.StringMap, opts ...pulumi.ResourceOption) (*Image, error) {
	component := &Image{}
	if err := ctx.RegisterComponentResource("lyrics:image:Image", name, component, opts...); err != nil {
		return nil, err
	}
	parent := pulumi.Parent(component)
	logs, err := cloudwatch.NewLogGroup(ctx, name+"-logs", &cloudwatch.LogGroupArgs{
		NamePrefix:      pulumi.String("/ecs/" + name + "-"),
		RetentionInDays: pulumi.Int(30),
		Tags:            tags,
	}, parent)
	if err != nil {
		return nil, err
	}

	container := pulumi.JSONMarshal(pulumi.All(image, storage.Bucket.Bucket, queue.Queue.Url, logs.Name).ApplyT(func(values []interface{}) []map[string]interface{} {
		return []map[string]interface{}{{
			"name":        "worker",
			"image":       values[0].(string),
			"essential":   true,
			"stopTimeout": 120,
			"healthCheck": map[string]interface{}{
				"command":     []string{"CMD-SHELL", "python healthcheck.py"},
				"interval":    60,
				"timeout":     5,
				"retries":     3,
				"startPeriod": 120,
			},
			"environment": []map[string]string{
				{"name": "AWS_REGION", "value": region},
				{"name": "MEDIA_BUCKET", "value": values[1].(string)},
				{"name": "QUEUE_URL", "value": values[2].(string)},
				{"name": "INPUT_PREFIX", "value": "input"},
				{"name": "OUTPUT_PREFIX", "value": "output"},
			},
			"logConfiguration": map[string]interface{}{
				"logDriver": "awslogs",
				"options": map[string]string{
					"awslogs-group":         values[3].(string),
					"awslogs-region":        region,
					"awslogs-stream-prefix": "worker",
				},
			},
		}}
	}))
	task, err := ecs.NewTaskDefinition(ctx, name+"-task", &ecs.TaskDefinitionArgs{
		Family:                  pulumi.String(name),
		Cpu:                     pulumi.String(cpu),
		Memory:                  pulumi.String(memory),
		NetworkMode:             pulumi.String("awsvpc"),
		RequiresCompatibilities: pulumi.StringArray{pulumi.String("FARGATE")},
		ExecutionRoleArn:        roles.ExecutionRole.Arn,
		TaskRoleArn:             roles.TaskRole.Arn,
		ContainerDefinitions:    container,
		Tags:                    tags,
	}, parent)
	if err != nil {
		return nil, err
	}
	component.LogGroup = logs
	component.TaskDefinition = task
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"logGroupName":      logs.Name,
		"taskDefinitionArn": task.Arn,
	}); err != nil {
		return nil, err
	}
	return component, nil
}
