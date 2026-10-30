// Package iam owns worker trust, execution and data-access permissions.
package iam

import (
	"LyricsPipeline/internal/config"

	awsiam "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const policyVersion = "2012-10-17"
const executionPolicyARN = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"

type Args struct {
	BucketARN pulumi.StringInput
	QueueARN  pulumi.StringInput
	Objects   config.ObjectLayout
	Tags      pulumi.StringMap
}

type Identity struct {
	pulumi.ResourceState
	ExecutionRoleARN pulumi.StringOutput
	TaskRoleARN      pulumi.StringOutput
	PermissionsReady []pulumi.Resource
}

type builder struct {
	ctx    *pulumi.Context
	name   string
	args   Args
	parent pulumi.ResourceOption
}

func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Identity, error) {
	component := &Identity{}
	if err := ctx.RegisterComponentResource("lyrics:iam:WorkerIAM", name, component, opts...); err != nil {
		return nil, err
	}
	b := builder{
		ctx:    ctx,
		name:   name,
		args:   args,
		parent: pulumi.Parent(component),
	}
	trust := b.trustPolicy()
	executionRole, err := awsiam.NewRole(ctx, name+"-execution-role", &awsiam.RoleArgs{
		AssumeRolePolicy: trust,
		Tags:             args.Tags,
	}, b.parent)
	if err != nil {
		return nil, err
	}
	executionPolicy, err := awsiam.NewRolePolicyAttachment(ctx, name+"-execution-policy", &awsiam.RolePolicyAttachmentArgs{
		Role:      executionRole.Name,
		PolicyArn: pulumi.String(executionPolicyARN),
	}, b.parent)
	if err != nil {
		return nil, err
	}
	taskRole, err := awsiam.NewRole(ctx, name+"-task-role", &awsiam.RoleArgs{
		AssumeRolePolicy: trust,
		Tags:             args.Tags,
	}, b.parent)
	if err != nil {
		return nil, err
	}
	taskPolicy, err := awsiam.NewRolePolicy(ctx, name+"-task-policy", &awsiam.RolePolicyArgs{
		Role:   taskRole.Name,
		Policy: b.taskPolicy(),
	}, b.parent)
	if err != nil {
		return nil, err
	}
	component.ExecutionRoleARN, component.TaskRoleARN = executionRole.Arn, taskRole.Arn
	component.PermissionsReady = []pulumi.Resource{executionPolicy, taskPolicy}
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{"executionRoleArn": component.ExecutionRoleARN, "taskRoleArn": component.TaskRoleARN}); err != nil {
		return nil, err
	}
	return component, nil
}

func (b *builder) trustPolicy() pulumi.StringOutput {
	return awsiam.GetPolicyDocumentOutput(b.ctx, awsiam.GetPolicyDocumentOutputArgs{
		Version: pulumi.String(policyVersion),
		Statements: awsiam.GetPolicyDocumentStatementArray{awsiam.GetPolicyDocumentStatementArgs{
			Effect:  pulumi.String("Allow"),
			Actions: pulumi.StringArray{pulumi.String("sts:AssumeRole")},
			Principals: awsiam.GetPolicyDocumentStatementPrincipalArray{awsiam.GetPolicyDocumentStatementPrincipalArgs{
				Type:        pulumi.String("Service"),
				Identifiers: pulumi.StringArray{pulumi.String("ecs-tasks.amazonaws.com")},
			}},
		}},
	}).Json()
}

func (b *builder) taskPolicy() pulumi.StringOutput {
	inputObjects := pulumi.Sprintf("%s/%s*", b.args.BucketARN, b.args.Objects.Input.NotificationPrefix())
	outputObjects := pulumi.Sprintf("%s/%s*", b.args.BucketARN, b.args.Objects.Output.NotificationPrefix())
	return awsiam.GetPolicyDocumentOutput(b.ctx, awsiam.GetPolicyDocumentOutputArgs{
		Version: pulumi.String(policyVersion),
		Statements: awsiam.GetPolicyDocumentStatementArray{
			awsiam.GetPolicyDocumentStatementArgs{
				Effect:    pulumi.String("Allow"),
				Actions:   pulumi.StringArray{pulumi.String("s3:ListBucket")},
				Resources: pulumi.StringArray{b.args.BucketARN},
			},
			awsiam.GetPolicyDocumentStatementArgs{
				Effect:    pulumi.String("Allow"),
				Actions:   pulumi.StringArray{pulumi.String("s3:GetObject")},
				Resources: pulumi.StringArray{inputObjects},
			},
			awsiam.GetPolicyDocumentStatementArgs{
				Effect:    pulumi.String("Allow"),
				Actions:   pulumi.StringArray{pulumi.String("s3:GetObject"), pulumi.String("s3:PutObject")},
				Resources: pulumi.StringArray{outputObjects},
			},
			awsiam.GetPolicyDocumentStatementArgs{
				Effect: pulumi.String("Allow"),
				Actions: pulumi.StringArray{
					pulumi.String("sqs:ReceiveMessage"), pulumi.String("sqs:DeleteMessage"), pulumi.String("sqs:ChangeMessageVisibility"), pulumi.String("sqs:GetQueueAttributes"),
				},
				Resources: pulumi.StringArray{b.args.QueueARN},
			},
		},
	}).Json()
}
