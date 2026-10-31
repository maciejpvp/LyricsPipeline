// Package ingestion connects object uploads to an existing processing queue.
package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type ingestionArgs struct {
	BucketID    pulumi.IDInput
	BucketARN   pulumi.StringInput
	QueueARN    pulumi.StringInput
	QueueURL    pulumi.StringInput
	InputPrefix ObjectPrefix
}

type Ingestion struct {
	Policy       *sqs.QueuePolicy
	Notification *s3.BucketNotification
}

// New deliberately registers no component: these resources retain their original
// queue parent and therefore their existing Pulumi URNs.
func configureIngestion(ctx *pulumi.Context, name string, args ingestionArgs, opts ...pulumi.ResourceOption) (*Ingestion, error) {
	policy := iam.GetPolicyDocumentOutput(ctx, iam.GetPolicyDocumentOutputArgs{
		Version: pulumi.String("2012-10-17"),
		Statements: iam.GetPolicyDocumentStatementArray{iam.GetPolicyDocumentStatementArgs{
			Effect:    pulumi.String("Allow"),
			Actions:   pulumi.StringArray{pulumi.String("sqs:SendMessage")},
			Resources: pulumi.StringArray{args.QueueARN},
			Principals: iam.GetPolicyDocumentStatementPrincipalArray{iam.GetPolicyDocumentStatementPrincipalArgs{
				Type:        pulumi.String("Service"),
				Identifiers: pulumi.StringArray{pulumi.String("s3.amazonaws.com")},
			}},
			Conditions: iam.GetPolicyDocumentStatementConditionArray{iam.GetPolicyDocumentStatementConditionArgs{
				Test:     pulumi.String("ArnEquals"),
				Variable: pulumi.String("aws:SourceArn"),
				Values:   pulumi.StringArray{args.BucketARN},
			}},
		}},
	})
	permission, err := sqs.NewQueuePolicy(ctx, name+"-queue-policy", &sqs.QueuePolicyArgs{
		QueueUrl: args.QueueURL,
		Policy:   policy.Json(),
	}, opts...)
	if err != nil {
		return nil, err
	}
	notificationOpts := append(append([]pulumi.ResourceOption{}, opts...), pulumi.DependsOn([]pulumi.Resource{permission}))
	notification, err := s3.NewBucketNotification(ctx, name+"-notification", &s3.BucketNotificationArgs{
		Bucket:      args.BucketID.ToIDOutput().ToStringOutput(),
		Eventbridge: pulumi.Bool(true),
		Queues: s3.BucketNotificationQueueArray{s3.BucketNotificationQueueArgs{
			Id:           pulumi.String("input-created"),
			QueueArn:     args.QueueARN,
			Events:       pulumi.StringArray{pulumi.String("s3:ObjectCreated:*")},
			FilterPrefix: pulumi.String(args.InputPrefix.NotificationPrefix()),
		}},
	}, notificationOpts...)
	if err != nil {
		return nil, err
	}
	return &Ingestion{
		Policy:       permission,
		Notification: notification,
	}, nil
}
