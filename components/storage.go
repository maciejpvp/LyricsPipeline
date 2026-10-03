package components

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type StorageArgs struct {
	Tags pulumi.StringMap
}

type Storage struct {
	pulumi.ResourceState

	Bucket     *s3.Bucket
	BucketName pulumi.StringOutput
}

func NewStorage(ctx *pulumi.Context, name string, args *StorageArgs, opts ...pulumi.ResourceOption) (*Storage, error) {
	if args == nil {
		args = &StorageArgs{}
	}

	component := &Storage{}
	if err := ctx.RegisterComponentResource("custom:storage:Storage", name, component, opts...); err != nil {
		return nil, err
	}

	bucket, err := s3.NewBucket(ctx, fmt.Sprintf("%s-bucket", name), &s3.BucketArgs{
		BucketPrefix: pulumi.String(fmt.Sprintf("%s-", name)),
		ForceDestroy: pulumi.Bool(true),
		Tags:         args.Tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, err
	}

	if _, err = s3.NewBucketPublicAccessBlock(ctx, fmt.Sprintf("%s-public-access-block", name), &s3.BucketPublicAccessBlockArgs{
		Bucket:                bucket.ID(),
		BlockPublicAcls:       pulumi.Bool(true),
		BlockPublicPolicy:     pulumi.Bool(true),
		IgnorePublicAcls:      pulumi.Bool(true),
		RestrictPublicBuckets: pulumi.Bool(true),
	}, pulumi.Parent(component)); err != nil {
		return nil, err
	}

	if _, err = s3.NewBucketOwnershipControls(ctx, fmt.Sprintf("%s-ownership-controls", name), &s3.BucketOwnershipControlsArgs{
		Bucket: bucket.ID(),
		Rule: s3.BucketOwnershipControlsRuleArgs{
			ObjectOwnership: pulumi.String("BucketOwnerEnforced"),
		},
	}, pulumi.Parent(component)); err != nil {
		return nil, err
	}

	if _, err = s3.NewBucketServerSideEncryptionConfiguration(ctx, fmt.Sprintf("%s-encryption", name), &s3.BucketServerSideEncryptionConfigurationArgs{
		Bucket: bucket.ID(),
		Rules: s3.BucketServerSideEncryptionConfigurationRuleArray{
			s3.BucketServerSideEncryptionConfigurationRuleArgs{
				ApplyServerSideEncryptionByDefault: s3.BucketServerSideEncryptionConfigurationRuleApplyServerSideEncryptionByDefaultArgs{
					SseAlgorithm: pulumi.String("AES256"),
				},
			},
		},
	}, pulumi.Parent(component)); err != nil {
		return nil, err
	}

	component.Bucket = bucket
	component.BucketName = bucket.Bucket

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"bucketName": component.BucketName,
	}); err != nil {
		return nil, err
	}

	return component, nil
}
