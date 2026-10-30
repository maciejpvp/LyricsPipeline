package main

import (
	"LyricsPipeline/internal/deployment"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() { pulumi.Run(deployment.Run) }
