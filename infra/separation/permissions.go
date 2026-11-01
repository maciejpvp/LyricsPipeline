package main

import (
	"LyricsPipeline/infra/contracts"
	"LyricsPipeline/internal/iam"

	awsiam "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const policyVersion = "2012-10-17"

type workerIdentity struct {
	pulumi.ResourceState
	ExecutionRoleARN, TaskRoleARN pulumi.StringOutput
	PermissionsReady              []pulumi.Resource
}

func createWorkerIdentity(ctx *pulumi.Context, name string, base contracts.Base, tags pulumi.StringMap) (*workerIdentity, error) {
	c := &workerIdentity{}
	if err := ctx.RegisterComponentResource("lyrics:iam:WorkerIAM", name, c); err != nil {
		return nil, err
	}
	parent := pulumi.Parent(c)
	execution, err := iam.NewRole(ctx, name+"-execution-role", iam.Args{
		Trust: workerTrust(ctx),
		Tags:  tags,
		ManagedPolicies: []iam.ManagedPolicy{{
			Name: name + "-execution-policy",
			ARN:  pulumi.String("arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"),
		}},
	}, parent)
	if err != nil {
		return nil, err
	}
	task, err := iam.NewRole(ctx, name+"-task-role", iam.Args{
		Trust: workerTrust(ctx),
		Tags:  tags,
		Policies: []iam.Policy{{
			Name:     name + "-task-policy",
			Document: workerPolicy(ctx, base),
		}},
	}, parent)
	if err != nil {
		return nil, err
	}
	c.ExecutionRoleARN, c.TaskRoleARN = execution.ARN, task.ARN
	c.PermissionsReady = append(execution.PermissionsReady, task.PermissionsReady...)
	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{"executionRoleArn": c.ExecutionRoleARN, "taskRoleArn": c.TaskRoleARN}); err != nil {
		return nil, err
	}
	return c, nil
}
func workerTrust(ctx *pulumi.Context) pulumi.StringOutput {
	return awsiam.GetPolicyDocumentOutput(ctx, awsiam.GetPolicyDocumentOutputArgs{
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

func workerPolicy(ctx *pulumi.Context, base contracts.Base) pulumi.StringOutput {
	inputObjects := pulumi.Sprintf("%s/%s/*", base.BucketARN, base.Objects.Input)
	outputObjects := pulumi.Sprintf("%s/%s/*", base.BucketARN, base.Objects.Output)
	return awsiam.GetPolicyDocumentOutput(ctx, awsiam.GetPolicyDocumentOutputArgs{
		Version: pulumi.String(policyVersion),
		Statements: awsiam.GetPolicyDocumentStatementArray{
			awsiam.GetPolicyDocumentStatementArgs{
				Effect:    pulumi.String("Allow"),
				Actions:   pulumi.StringArray{pulumi.String("s3:ListBucket")},
				Resources: pulumi.StringArray{base.BucketARN},
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
				Resources: pulumi.StringArray{base.QueueARN},
			},
		},
	}).Json()
}
