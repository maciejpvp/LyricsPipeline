// Package lambda creates Lambda functions from already built artifacts and explicit AWS inputs.
package lambda

import (
	awslambda "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Args struct {
	Artifact                       pulumi.ArchiveInput
	Runtime, Handler, Architecture string
	RoleARN, LogGroup              pulumi.StringInput
	TimeoutSeconds, MemoryMiB      int
	Environment                    pulumi.StringMap
	Tags                           pulumi.StringMap
}

func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*awslambda.Function, error) {
	return awslambda.NewFunction(ctx, name, &awslambda.FunctionArgs{
		Runtime:       pulumi.String(args.Runtime),
		Handler:       pulumi.String(args.Handler),
		Architectures: pulumi.StringArray{pulumi.String(args.Architecture)},
		Role:          args.RoleARN,
		Code:          args.Artifact,
		Timeout:       pulumi.Int(args.TimeoutSeconds),
		MemorySize:    pulumi.Int(args.MemoryMiB),
		LoggingConfig: &awslambda.FunctionLoggingConfigArgs{
			LogFormat: pulumi.String("Text"),
			LogGroup:  args.LogGroup,
		},
		Environment: &awslambda.FunctionEnvironmentArgs{Variables: args.Environment},
		Tags:        args.Tags,
	}, opts...)
}
