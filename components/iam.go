package components

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type WorkerIAM struct {
	pulumi.ResourceState
	ExecutionRole *iam.Role
	TaskRole      *iam.Role
}

func NewWorkerIAM(ctx *pulumi.Context, name string, storage *Storage, queue *Queue, tags pulumi.StringMap, opts ...pulumi.ResourceOption) (*WorkerIAM, error) {
	component := &WorkerIAM{}
	if err := ctx.RegisterComponentResource("lyrics:iam:WorkerIAM", name, component, opts...); err != nil {
		return nil, err
	}
	parent := pulumi.Parent(component)
	assumeRolePolicy, err := iam.GetPolicyDocument(ctx, &iam.GetPolicyDocumentArgs{
		Version: pulumi.StringRef("2012-10-17"),
		Statements: []iam.GetPolicyDocumentStatement{
			{
				Effect:  pulumi.StringRef("Allow"),
				Actions: []string{"sts:AssumeRole"},
				Principals: []iam.GetPolicyDocumentStatementPrincipal{
					{
						Type:        "Service",
						Identifiers: []string{"ecs-tasks.amazonaws.com"},
					},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	executionRole, err := iam.NewRole(
		ctx,
		name+"-execution-role",
		&iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(assumeRolePolicy.Json),
			Tags:             tags,
		}, parent,
	)
	if err != nil {
		return nil, err
	}

	executionPolicyAttachment := &iam.RolePolicyAttachmentArgs{
		Role:      executionRole.Name,
		PolicyArn: pulumi.String("arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"),
	}
	if _, err = iam.NewRolePolicyAttachment(
		ctx,
		name+"-execution-policy",
		executionPolicyAttachment,
		parent,
	); err != nil {
		return nil, err
	}

	taskRole, err := iam.NewRole(
		ctx,
		name+"-task-role",
		&iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(assumeRolePolicy.Json),
			Tags:             tags,
		}, parent,
	)
	if err != nil {
		return nil, err
	}

	policy := pulumi.JSONMarshal(pulumi.All(storage.Bucket.Arn, queue.Queue.Arn).ApplyT(func(values []interface{}) map[string]interface{} {
		bucketARN := values[0].(string)
		queueARN := values[1].(string)
		return map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				{"Effect": "Allow", "Action": []string{"s3:ListBucket"}, "Resource": bucketARN},
				{"Effect": "Allow", "Action": []string{"s3:GetObject"}, "Resource": bucketARN + "/input/*"},
				{"Effect": "Allow", "Action": []string{"s3:GetObject", "s3:PutObject"}, "Resource": bucketARN + "/output/*"},
				{"Effect": "Allow", "Action": []string{
					"sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:ChangeMessageVisibility", "sqs:GetQueueAttributes",
				}, "Resource": queueARN},
			},
		}
	}))

	taskPolicyArgs := &iam.RolePolicyArgs{
		Role:   taskRole.Name,
		Policy: policy,
	}
	if _, err = iam.NewRolePolicy(ctx, name+"-task-policy", taskPolicyArgs, parent); err != nil {
		return nil, err
	}
	component.ExecutionRole = executionRole
	component.TaskRole = taskRole
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"executionRoleArn": executionRole.Arn,
		"taskRoleArn":      taskRole.Arn,
	}); err != nil {
		return nil, err
	}
	return component, nil
}
