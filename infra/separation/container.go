package main

import "time"

// These are JSON documents consumed by ECS, not maps of loosely typed properties.
type containerDefinition struct {
	Name             string                `json:"name"`
	Image            string                `json:"image"`
	Essential        bool                  `json:"essential"`
	StopTimeout      int                   `json:"stopTimeout"`
	HealthCheck      containerHealthCheck  `json:"healthCheck"`
	Environment      []environmentVariable `json:"environment"`
	LogConfiguration logConfiguration      `json:"logConfiguration"`
}

type containerHealthCheck struct {
	Command     []string `json:"command"`
	Interval    int      `json:"interval"`
	Timeout     int      `json:"timeout"`
	Retries     int      `json:"retries"`
	StartPeriod int      `json:"startPeriod"`
}

type environmentVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type logConfiguration struct {
	LogDriver string     `json:"logDriver"`
	Options   logOptions `json:"options"`
}
type logOptions struct {
	Group        string `json:"awslogs-group"`
	Region       string `json:"awslogs-region"`
	StreamPrefix string `json:"awslogs-stream-prefix"`
}

func (b taskBuilder) container(image, bucket, queueURL, logGroup string) []containerDefinition {
	health := b.settings.HealthCheck
	return []containerDefinition{{
		Name:        "worker",
		Image:       image,
		Essential:   true,
		StopTimeout: int(b.settings.StopTimeout / time.Second),
		HealthCheck: containerHealthCheck{
			Command:     health.Command,
			Interval:    int(health.Interval / time.Second),
			Timeout:     int(health.Timeout / time.Second),
			Retries:     health.Retries,
			StartPeriod: int(health.StartPeriod / time.Second),
		},
		Environment: []environmentVariable{
			{Name: "AWS_REGION", Value: b.region},
			{Name: "MEDIA_BUCKET", Value: bucket},
			{Name: "QUEUE_URL", Value: queueURL},
		},
		LogConfiguration: logConfiguration{
			LogDriver: "awslogs",
			Options: logOptions{
				Group:        logGroup,
				Region:       b.region,
				StreamPrefix: "worker",
			},
		},
	}}
}
