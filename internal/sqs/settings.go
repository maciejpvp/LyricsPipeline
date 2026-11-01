package sqs

import "time"

type Settings struct {
	VisibilityTimeout   time.Duration
	MaxReceiveCount     int
	LongPollWait        time.Duration
	DeadLetterRetention time.Duration
}
