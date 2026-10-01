package components

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ecs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Image struct {
	LogGroup       *cloudwatch.LogGroup
	TaskDefinition *ecs.TaskDefinition
}

func NewImage(ctx *pulumi.Context, name, image, region, cpu, memory string, storage *Storage, queue *Queue, roles *WorkerIAM, tags pulumi.StringMap) (*Image, error) {
	logs, err := cloudwatch.NewLogGroup(ctx, name+"-logs", &cloudwatch.LogGroupArgs{
		NamePrefix:      pulumi.String("/ecs/" + name + "-"),
		RetentionInDays: pulumi.Int(30),
		Tags:            tags,
	})
	if err != nil {
		return nil, err
	}

	container := pulumi.Sprintf(`[{"name":"worker","image":%q,"essential":true,"environment":[{"name":"AWS_REGION","value":%q},{"name":"MEDIA_BUCKET","value":%q},{"name":"QUEUE_URL","value":%q},{"name":"INPUT_PREFIX","value":"input"},{"name":"OUTPUT_PREFIX","value":"output"}],"logConfiguration":{"logDriver":"awslogs","options":{"awslogs-group":%q,"awslogs-region":%q,"awslogs-stream-prefix":"worker"}}}]`, image, region, storage.Bucket.Bucket, queue.Queue.Url, logs.Name, region)
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
	})
	if err != nil {
		return nil, err
	}
	return &Image{LogGroup: logs, TaskDefinition: task}, nil
}
