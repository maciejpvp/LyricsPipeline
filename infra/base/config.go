package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"LyricsPipeline/internal/config"
	"LyricsPipeline/internal/sqs"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	pc "github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

type deploymentConfig struct {
	Name, Region string
	Tags         map[string]string
	Queue        sqs.Settings
	Objects      ObjectLayout
}
type ObjectPrefix string

func (p ObjectPrefix) NotificationPrefix() string { return string(p) + "/" }

type ObjectLayout struct {
	Input  ObjectPrefix
	Output ObjectPrefix
}

func (o ObjectLayout) Validate() error {
	for _, field := range []struct {
		name   string
		prefix ObjectPrefix
	}{{"inputPrefix", o.Input}, {"outputPrefix", o.Output}} {
		prefix := string(field.prefix)
		if strings.TrimSpace(prefix) == "" || strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/") {
			return fmt.Errorf("s3.%s: expected a non-empty prefix without leading or trailing slash", field.name)
		}
	}
	if o.Input == o.Output || strings.HasPrefix(string(o.Output), o.Input.NotificationPrefix()) {
		return errors.New("s3.outputPrefix: must not be inside inputPrefix, to avoid triggering input notifications")
	}
	return nil
}

type s3Settings struct {
	InputPrefix  ObjectPrefix `json:"inputPrefix"`
	OutputPrefix ObjectPrefix `json:"outputPrefix"`
}
type sqsSettings struct {
	VisibilitySeconds       int `json:"visibilitySeconds"`
	MaxReceiveCount         int `json:"maxReceiveCount"`
	LongPollWaitSeconds     int `json:"longPollWaitSeconds"`
	DeadLetterRetentionDays int `json:"deadLetterRetentionDays"`
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

func (s *s3Settings) UnmarshalJSON(data []byte) error {
	type fields s3Settings
	return config.DecodeObject(data, (*fields)(s))
}
func (s *sqsSettings) UnmarshalJSON(data []byte) error {
	type fields sqsSettings
	return config.DecodeObject(data, (*fields)(s))
}
func (s sqsSettings) apply(queue *sqs.Settings) error {
	const maximumVisibility = 12 * time.Hour
	if s.VisibilitySeconds < 1 || s.VisibilitySeconds > int(maximumVisibility/time.Second) {
		return errors.New("sqs.visibilitySeconds: must be between 1 and 43200")
	}
	if s.LongPollWaitSeconds < 0 || s.LongPollWaitSeconds > 20 {
		return errors.New("sqs.longPollWaitSeconds: must be between 0 and 20")
	}
	if s.DeadLetterRetentionDays < 1 || s.DeadLetterRetentionDays > 14 {
		return errors.New("sqs.deadLetterRetentionDays: must be between 1 and 14")
	}
	queue.VisibilityTimeout = time.Duration(s.VisibilitySeconds) * time.Second
	queue.MaxReceiveCount = s.MaxReceiveCount
	queue.LongPollWait = time.Duration(s.LongPollWaitSeconds) * time.Second
	queue.DeadLetterRetention = time.Duration(s.DeadLetterRetentionDays) * 24 * time.Hour
	return nil
}
func loadConfig(ctx *pulumi.Context) (deploymentConfig, error) {
	d, err := loadMetadata(ctx)
	if err != nil {
		return d, err
	}
	c := pc.New(ctx, "")
	var objects s3Settings
	if err = c.TryObject("s3", &objects); err != nil {
		return d, fmt.Errorf("s3: %w", err)
	}
	d.Objects = ObjectLayout{Input: objects.InputPrefix, Output: objects.OutputPrefix}
	if err = d.Objects.Validate(); err != nil {
		return d, err
	}
	// EventBridge wildcard patterns must treat the configured prefix literally.
	if strings.ContainsAny(string(d.Objects.Output), "*?\\") {
		return d, errors.New("s3.outputPrefix cannot contain wildcard characters")
	}
	var queue sqsSettings
	if err = c.TryObject("sqs", &queue); err != nil {
		return d, fmt.Errorf("sqs: %w", err)
	}
	if err = queue.apply(&d.Queue); err != nil {
		return d, err
	}
	if d.Queue.MaxReceiveCount < 2 || d.Queue.MaxReceiveCount > 1000 {
		return d, errors.New("sqs.maxReceiveCount must be between 2 and 1000")
	}
	return d, nil
}
