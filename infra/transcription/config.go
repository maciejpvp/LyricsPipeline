package main

import (
	"errors"
	"fmt"
	"strings"

	"LyricsPipeline/internal/config"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	pc "github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

type deploymentConfig struct {
	BaseStack     string
	Name          string
	Region        string
	Tags          map[string]string
	Transcription TranscriptionSettings
}

type TranscriptionSettings struct {
	Language                string `json:"language"`
	PipelineVersion         string `json:"pipelineVersion"`
	PollSeconds             int    `json:"pollSeconds"`
	TimeoutSeconds          int    `json:"timeoutSeconds"`
	RetryAttempts           int    `json:"retryAttempts"`
	RetryIntervalSeconds    int    `json:"retryIntervalSeconds"`
	LambdaTimeoutSeconds    int    `json:"lambdaTimeoutSeconds"`
	LambdaMemoryMiB         int    `json:"lambdaMemoryMiB"`
	LogRetentionDays        int    `json:"logRetentionDays"`
	DeadLetterRetentionDays int    `json:"deadLetterRetentionDays"`
	EventMaxAgeSeconds      int    `json:"eventMaxAgeSeconds"`
	EventRetryAttempts      int    `json:"eventRetryAttempts"`
}

func (s *TranscriptionSettings) UnmarshalJSON(data []byte) error {
	type fields TranscriptionSettings
	return config.DecodeObject(data, (*fields)(s))
}
func (s TranscriptionSettings) Validate() error {
	if s.Language != "en-US" || s.PipelineVersion == "" {
		return fmt.Errorf("transcription requires en-US and a nonempty pipelineVersion")
	}
	if s.PollSeconds < 1 || s.TimeoutSeconds <= s.PollSeconds || s.TimeoutSeconds > 31536000 || s.RetryAttempts < 1 || s.RetryAttempts > 5 || s.RetryIntervalSeconds < 1 || s.RetryIntervalSeconds > 99999999 {
		return fmt.Errorf("invalid transcription polling, timeout or retry configuration")
	}
	if s.LambdaTimeoutSeconds < 1 || s.LambdaTimeoutSeconds > 900 || s.LambdaMemoryMiB < 128 || s.LambdaMemoryMiB > 10240 {
		return fmt.Errorf("invalid Lambda resources")
	}
	if s.LogRetentionDays < 1 || s.DeadLetterRetentionDays < 1 || s.DeadLetterRetentionDays > 14 || s.EventMaxAgeSeconds < 60 || s.EventMaxAgeSeconds > 86400 || s.EventRetryAttempts < 0 || s.EventRetryAttempts > 185 {
		return fmt.Errorf("invalid log retention, DLQ or EventBridge retry configuration")
	}
	return nil
}

func loadConfig(ctx *pulumi.Context) (deploymentConfig, error) {
	metadata, err := loadMetadata(ctx)
	if err != nil {
		return deploymentConfig{}, err
	}
	baseStack, err := pc.New(ctx, "").Try("baseStack")
	if err != nil || strings.TrimSpace(baseStack) == "" {
		return deploymentConfig{}, fmt.Errorf("baseStack is required: %v", err)
	}
	var settings TranscriptionSettings
	if err = pc.New(ctx, "").TryObject("transcription", &settings); err != nil {
		return deploymentConfig{}, fmt.Errorf("transcription: %w", err)
	}
	if err = settings.Validate(); err != nil {
		return deploymentConfig{}, err
	}
	return deploymentConfig{BaseStack: baseStack, Name: metadata.Name, Region: metadata.Region, Tags: metadata.Tags, Transcription: settings}, nil
}
func loadMetadata(ctx *pulumi.Context) (deploymentConfig, error) {
	c := pc.New(ctx, "")
	var d deploymentConfig
	var err error
	for _, key := range []string{"deployment", "deploymentImage"} {
		if _, e := c.Try(key); e == nil {
			return d, fmt.Errorf("legacy configuration %s: migrate configuration before splitting stacks", key)
		}
	}
	if d.Name, err = c.Try("name"); err != nil {
		return d, err
	}
	if d.Region, err = pc.New(ctx, "aws").Try("region"); err != nil {
		return d, err
	}
	if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Region) == "" {
		return d, errors.New("name and aws:region must not be empty")
	}
	if err = c.TryObject("tags", &d.Tags); err != nil {
		return d, err
	}
	if d.Tags == nil {
		return d, errors.New("tags must be an object")
	}
	return d, nil
}
