package components

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Queue contains the worker queue, dead-letter queue, and S3 notification wiring.
// The S3 bucket itself belongs to Storage.
type Queue struct {
	Queue      *sqs.Queue
	DeadLetter *sqs.Queue
}

func NewQueue(ctx *pulumi.Context, name string, storage *Storage, maxReceiveCount, visibility int, tags pulumi.StringMap) (*Queue, error) {
	deadLetter, err := sqs.NewQueue(ctx, name+"-dlq", &sqs.QueueArgs{
		NamePrefix:              pulumi.String(name + "-dlq-"),
		MessageRetentionSeconds: pulumi.Int(1209600),
		SqsManagedSseEnabled:    pulumi.Bool(true),
		Tags:                    tags,
	})
	if err != nil {
		return nil, err
	}

	redrive := pulumi.Sprintf(`{"deadLetterTargetArn":%q,"maxReceiveCount":%d}`, deadLetter.Arn, maxReceiveCount)
	queue, err := sqs.NewQueue(ctx, name+"-queue", &sqs.QueueArgs{
		NamePrefix:               pulumi.String(name + "-"),
		VisibilityTimeoutSeconds: pulumi.Int(visibility),
		ReceiveWaitTimeSeconds:   pulumi.Int(20),
		RedrivePolicy:            redrive,
		SqsManagedSseEnabled:     pulumi.Bool(true),
		Tags:                     tags,
	})
	if err != nil {
		return nil, err
	}

	policy := pulumi.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"s3.amazonaws.com"},"Action":"sqs:SendMessage","Resource":%q,"Condition":{"ArnEquals":{"aws:SourceArn":%q}}}]}`, queue.Arn, storage.Bucket.Arn)
	if _, err = sqs.NewQueuePolicy(ctx, name+"-queue-policy", &sqs.QueuePolicyArgs{
		QueueUrl: queue.Url,
		Policy:   policy,
	}); err != nil {
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
	}); err != nil {
		return nil, err
	}

	return &Queue{Queue: queue, DeadLetter: deadLetter}, nil
}
