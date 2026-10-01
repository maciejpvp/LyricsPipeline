package components

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ecr"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Registry struct {
	Repository *ecr.Repository
}

func NewRegistry(ctx *pulumi.Context, name string, tags pulumi.StringMap) (*Registry, error) {
	repository, err := ecr.NewRepository(ctx, name+"-image", &ecr.RepositoryArgs{
		ForceDelete: pulumi.Bool(false),
		ImageScanningConfiguration: ecr.RepositoryImageScanningConfigurationArgs{
			ScanOnPush: pulumi.Bool(true),
		},
		Tags: tags,
	})
	if err != nil {
		return nil, err
	}
	return &Registry{Repository: repository}, nil
}
