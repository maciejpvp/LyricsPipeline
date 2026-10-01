package components

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type WorkerIAM struct {
	ExecutionRole *iam.Role
	TaskRole      *iam.Role
}

func NewWorkerIAM(ctx *pulumi.Context, name string, storage *Storage, queue *Queue, tags pulumi.StringMap) (*WorkerIAM, error) {
	assume := pulumi.String(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ecs-tasks.amazonaws.com"},"Action":"sts:AssumeRole"}]}`)
	executionRole, err := iam.NewRole(ctx, name+"-execution-role", &iam.RoleArgs{AssumeRolePolicy: assume, Tags: tags})
	if err != nil {
		return nil, err
	}
	if _, err = iam.NewRolePolicyAttachment(ctx, name+"-execution-policy", &iam.RolePolicyAttachmentArgs{Role: executionRole.Name, PolicyArn: pulumi.String("arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy")}); err != nil {
		return nil, err
	}
	taskRole, err := iam.NewRole(ctx, name+"-task-role", &iam.RoleArgs{AssumeRolePolicy: assume, Tags: tags})
	if err != nil {
		return nil, err
	}
	policy := pulumi.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":%q},{"Effect":"Allow","Action":["s3:PutObject"],"Resource":%q},{"Effect":"Allow","Action":["sqs:ReceiveMessage","sqs:DeleteMessage","sqs:ChangeMessageVisibility","sqs:GetQueueAttributes"],"Resource":%q}]}`, pulumi.Sprintf("%s/input/*", storage.Bucket.Arn), pulumi.Sprintf("%s/output/*", storage.Bucket.Arn), queue.Queue.Arn)
	if _, err = iam.NewRolePolicy(ctx, name+"-task-policy", &iam.RolePolicyArgs{Role: taskRole.Name, Policy: policy}); err != nil {
		return nil, err
	}
	return &WorkerIAM{ExecutionRole: executionRole, TaskRole: taskRole}, nil
}
