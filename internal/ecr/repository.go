package ecr

import (
	awsecr "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ecr"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Args struct{ Tags pulumi.StringMap }

type Registry struct {
	pulumi.ResourceState
	RepositoryURL pulumi.StringOutput
}

func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Registry, error) {
	component := &Registry{}
	if err := ctx.RegisterComponentResource("lyrics:registry:Registry", name, component, opts...); err != nil {
		return nil, err
	}
	repository, err := awsecr.NewRepository(ctx, name+"-image", &awsecr.RepositoryArgs{
		ForceDelete: pulumi.Bool(true),
		ImageScanningConfiguration: awsecr.RepositoryImageScanningConfigurationArgs{
			ScanOnPush: pulumi.Bool(true),
		},
		Tags: args.Tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, err
	}
	component.RepositoryURL = repository.RepositoryUrl
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{"repositoryUrl": repository.RepositoryUrl}); err != nil {
		return nil, err
	}
	return component, nil
}
