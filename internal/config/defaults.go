package config

import (
	"net/netip"
	"time"
)

func Defaults() Deployment {
	return Deployment{
		Name:             "vocal-extractor",
		Region:           "eu-central-1",
		AvailabilityZone: "eu-central-1a",
		Tags:             map[string]string{"Purpose": "vocal-extractor"},
		Network: Network{
			VPC:           netip.MustParsePrefix("10.42.0.0/16"),
			PublicSubnet:  netip.MustParsePrefix("10.42.1.0/24"),
			PrivateSubnet: netip.MustParsePrefix("10.42.2.0/24"),
			Internet:      netip.MustParsePrefix("0.0.0.0/0"),
		},
		Queue: Queue{
			VisibilityTimeout:   15 * time.Minute,
			MaxReceiveCount:     5,
			LongPollWait:        20 * time.Second,
			DeadLetterRetention: 14 * 24 * time.Hour,
		},
		Task: Task{
			Resources: TaskResources{
				CPU:    2 * CPUUnitsPerVCPU,
				Memory: 8 * MiBPerGiB,
			},
			StopTimeout:  2 * time.Minute,
			LogRetention: 30 * 24 * time.Hour,
			HealthCheck: HealthCheck{
				Command:     []string{"CMD-SHELL", "python healthcheck.py"},
				Interval:    time.Minute,
				Timeout:     5 * time.Second,
				Retries:     3,
				StartPeriod: 2 * time.Minute,
			},
		},
		Scaling: Scaling{
			MinTasks:         0,
			MaxTasks:         2,
			IdleWindow:       5 * time.Minute,
			BacklogPerWorker: 5,
			ScaleOutCooldown: 5 * time.Minute,
			ScaleInCooldown:  2 * time.Minute,
		},
		Objects: ObjectLayout{
			Input:  "input",
			Output: "output",
		},
	}
}
