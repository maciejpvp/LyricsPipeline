package components

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ecr"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Registry struct {
	pulumi.ResourceState
	Repository *ecr.Repository
}

func NewRegistry(ctx *pulumi.Context, name string, tags pulumi.StringMap, opts ...pulumi.ResourceOption) (*Registry, error) {
	component := &Registry{}
	if err := ctx.RegisterComponentResource("lyrics:registry:Registry", name, component, opts...); err != nil {
		return nil, err
	}
	repository, err := ecr.NewRepository(ctx, name+"-image", &ecr.RepositoryArgs{
		ForceDelete: pulumi.Bool(true),
		ImageScanningConfiguration: ecr.RepositoryImageScanningConfigurationArgs{
			ScanOnPush: pulumi.Bool(true),
		},
		Tags: tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, err
	}
	component.Repository = repository
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{"repositoryUrl": repository.RepositoryUrl}); err != nil {
		return nil, err
	}
	return component, nil
}
