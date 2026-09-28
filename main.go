package main

import (
	"LyricsPipeline/components"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		songsBucket, err := components.NewStorage(ctx, "songs-bucket", &components.StorageArgs{
			Tags: pulumi.StringMap{
				"Purpose": pulumi.String("songs-bucket"),
			},
		})
		if err != nil {
			return err
		}

		ctx.Export("songsBucket", songsBucket.BucketName)

		return nil
	})
}
