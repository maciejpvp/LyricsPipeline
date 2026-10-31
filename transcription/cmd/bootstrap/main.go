package main

import (
	"context"
	"log"

	"LyricsPipeline/transcription"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	settings, err := transcription.SettingsFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	handler := transcription.Handler{Store: transcription.S3Store{Client: s3.NewFromConfig(cfg)}, Settings: settings}
	lambda.Start(handler.Handle)
}
