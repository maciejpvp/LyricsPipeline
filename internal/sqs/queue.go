// Package sqs owns the processing queue and its dead-letter queue.
package sqs

import (
	"time"

	"LyricsPipeline/internal/config"

	awssqs "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumix"
)

type Args struct {
	Settings config.Queue
	Tags     pulumi.StringMap
}

type Queue struct {
	pulumi.ResourceState
	Name          pulumi.StringOutput
	URL           pulumi.StringOutput
	ARN           pulumi.StringOutput
	DeadLetterARN pulumi.StringOutput
}

type redrivePolicy struct {
	DeadLetterTargetARN string `json:"deadLetterTargetArn"`
	MaxReceiveCount     int    `json:"maxReceiveCount"`
}

func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Queue, error) {
	component := &Queue{}
	if err := ctx.RegisterComponentResource("lyrics:queue:Queue", name, component, opts...); err != nil {
		return nil, err
	}
	parent := pulumi.Parent(component)
	deadLetter, err := awssqs.NewQueue(ctx, name+"-dlq", &awssqs.QueueArgs{
		NamePrefix:              pulumi.String(name + "-dlq-"),
		MessageRetentionSeconds: pulumi.Int(args.Settings.DeadLetterRetention / time.Second),
		SqsManagedSseEnabled:    pulumi.Bool(true),
		Tags:                    args.Tags,
	}, parent)
	if err != nil {
		return nil, err
	}
	redrive := pulumi.JSONMarshal(pulumix.Apply(deadLetter.Arn, func(arn string) redrivePolicy {
		return redrivePolicy{
			DeadLetterTargetARN: arn,
			MaxReceiveCount:     args.Settings.MaxReceiveCount,
		}
	}))
	queue, err := awssqs.NewQueue(ctx, name+"-queue", &awssqs.QueueArgs{
		NamePrefix:               pulumi.String(name + "-"),
		VisibilityTimeoutSeconds: pulumi.Int(args.Settings.VisibilityTimeout / time.Second),
		ReceiveWaitTimeSeconds:   pulumi.Int(args.Settings.LongPollWait / time.Second),
		RedrivePolicy:            redrive,
		SqsManagedSseEnabled:     pulumi.Bool(true),
		Tags:                     args.Tags,
	}, parent)
	if err != nil {
		return nil, err
	}
	component.Name, component.URL, component.ARN, component.DeadLetterARN = queue.Name, queue.Url, queue.Arn, deadLetter.Arn
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{"queueUrl": component.URL, "queueArn": component.ARN, "deadLetterArn": component.DeadLetterARN}); err != nil {
		return nil, err
	}
	return component, nil
}
