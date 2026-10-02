package components

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Queue contains the worker queue, dead-letter queue, and S3 notification wiring.
// The S3 bucket itself belongs to Storage.
type Queue struct {
	pulumi.ResourceState
	Queue      *sqs.Queue
	DeadLetter *sqs.Queue
}

func NewQueue(ctx *pulumi.Context, name string, storage *Storage, maxReceiveCount, visibility int, tags pulumi.StringMap, opts ...pulumi.ResourceOption) (*Queue, error) {
	component := &Queue{}
	if err := ctx.RegisterComponentResource("lyrics:queue:Queue", name, component, opts...); err != nil {
		return nil, err
	}
	parent := pulumi.Parent(component)
	deadLetter, err := sqs.NewQueue(ctx, name+"-dlq", &sqs.QueueArgs{
		NamePrefix:              pulumi.String(name + "-dlq-"),
		MessageRetentionSeconds: pulumi.Int(1209600),
		SqsManagedSseEnabled:    pulumi.Bool(true),
		Tags:                    tags,
	}, parent)
	if err != nil {
		return nil, err
	}

	redrive := pulumi.JSONMarshal(pulumi.All(deadLetter.Arn, pulumi.Int(maxReceiveCount)).ApplyT(func(values []interface{}) map[string]interface{} {
		return map[string]interface{}{
			"deadLetterTargetArn": values[0].(string),
			"maxReceiveCount":     values[1].(int),
		}
	}))
	queue, err := sqs.NewQueue(ctx, name+"-queue", &sqs.QueueArgs{
		NamePrefix:               pulumi.String(name + "-"),
		VisibilityTimeoutSeconds: pulumi.Int(visibility),
		ReceiveWaitTimeSeconds:   pulumi.Int(20),
		RedrivePolicy:            redrive,
		SqsManagedSseEnabled:     pulumi.Bool(true),
		Tags:                     tags,
	}, parent)
	if err != nil {
		return nil, err
	}

	policy := pulumi.JSONMarshal(pulumi.All(queue.Arn, storage.Bucket.Arn).ApplyT(func(values []interface{}) map[string]interface{} {
		return map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{{
				"Effect":    "Allow",
				"Principal": map[string]interface{}{"Service": "s3.amazonaws.com"},
				"Action":    "sqs:SendMessage",
				"Resource":  values[0].(string),
				"Condition": map[string]interface{}{"ArnEquals": map[string]interface{}{"aws:SourceArn": values[1].(string)}},
			}},
		}
	}))
	if _, err = sqs.NewQueuePolicy(ctx, name+"-queue-policy", &sqs.QueuePolicyArgs{
		QueueUrl: queue.Url,
		Policy:   policy,
	}, parent); err != nil {
		return nil, err
	}

	if _, err = s3.NewBucketNotification(ctx, name+"-notification", &s3.BucketNotificationArgs{
		Bucket: storage.Bucket.ID(),
		Queues: s3.BucketNotificationQueueArray{
			s3.BucketNotificationQueueArgs{
				Id:           pulumi.String("input-created"),
				QueueArn:     queue.Arn,
				Events:       pulumi.StringArray{pulumi.String("s3:ObjectCreated:*")},
				FilterPrefix: pulumi.String("input/"),
			},
		},
	}, parent); err != nil {
		return nil, err
	}

	component.Queue = queue
	component.DeadLetter = deadLetter
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"queueUrl":      queue.Url,
		"queueArn":      queue.Arn,
		"deadLetterArn": deadLetter.Arn,
	}); err != nil {
		return nil, err
	}
	return component, nil
}
