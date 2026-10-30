package s3

import (
	awss3 "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Args struct {
	Tags pulumi.StringMap
}

type Storage struct {
	pulumi.ResourceState

	BucketID   pulumi.IDOutput
	BucketARN  pulumi.StringOutput
	BucketName pulumi.StringOutput
}

func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Storage, error) {
	component := &Storage{}
	if err := ctx.RegisterComponentResource("custom:storage:Storage", name, component, opts...); err != nil {
		return nil, err
	}

	bucket, err := awss3.NewBucket(ctx, name+"-bucket", &awss3.BucketArgs{
		BucketPrefix: pulumi.String(name + "-"),
		ForceDestroy: pulumi.Bool(true),
		Tags:         args.Tags,
	}, pulumi.Parent(component))
	if err != nil {
		return nil, err
	}

	b := builder{ctx: ctx, name: name, parent: pulumi.Parent(component)}
	if err := b.protect(bucket); err != nil {
		return nil, err
	}

	component.BucketID = bucket.ID()
	component.BucketARN = bucket.Arn
	component.BucketName = bucket.Bucket

	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"bucketName": component.BucketName,
	}); err != nil {
		return nil, err
	}

	return component, nil
}

type builder struct {
	ctx    *pulumi.Context
	name   string
	parent pulumi.ResourceOption
}

func (b *builder) protect(bucket *awss3.Bucket) error {
	if _, err := awss3.NewBucketPublicAccessBlock(b.ctx, b.name+"-public-access-block", &awss3.BucketPublicAccessBlockArgs{
		Bucket:                bucket.ID(),
		BlockPublicAcls:       pulumi.Bool(true),
		BlockPublicPolicy:     pulumi.Bool(true),
		IgnorePublicAcls:      pulumi.Bool(true),
		RestrictPublicBuckets: pulumi.Bool(true),
	}, b.parent); err != nil {
		return err
	}

	if _, err := awss3.NewBucketOwnershipControls(b.ctx, b.name+"-ownership-controls", &awss3.BucketOwnershipControlsArgs{
		Bucket: bucket.ID(),
		Rule: awss3.BucketOwnershipControlsRuleArgs{
			ObjectOwnership: pulumi.String("BucketOwnerEnforced"),
		},
	}, b.parent); err != nil {
		return err
	}

	if _, err := awss3.NewBucketServerSideEncryptionConfiguration(b.ctx, b.name+"-encryption", &awss3.BucketServerSideEncryptionConfigurationArgs{
		Bucket: bucket.ID(),
		Rules: awss3.BucketServerSideEncryptionConfigurationRuleArray{
			awss3.BucketServerSideEncryptionConfigurationRuleArgs{
				ApplyServerSideEncryptionByDefault: awss3.BucketServerSideEncryptionConfigurationRuleApplyServerSideEncryptionByDefaultArgs{
					SseAlgorithm: pulumi.String("AES256"),
				},
			},
		},
	}, b.parent); err != nil {
		return err
	}

	return nil
}
