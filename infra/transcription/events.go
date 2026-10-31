package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sfn"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumix"
)

type objectPattern struct {
	Source     []string     `json:"source"`
	DetailType []string     `json:"detail-type"`
	Detail     objectDetail `json:"detail"`
}
type objectDetail struct {
	Bucket bucketPattern `json:"bucket"`
	Object keyPattern    `json:"object"`
}
type bucketPattern struct {
	Name []string `json:"name"`
}
type keyPattern struct {
	Key []wildcard `json:"key"`
}
type wildcard struct {
	Wildcard string `json:"wildcard"`
}
type failurePattern struct {
	Source     []string      `json:"source"`
	DetailType []string      `json:"detail-type"`
	Detail     failureDetail `json:"detail"`
}
type failureDetail struct {
	StateMachineARN []string `json:"stateMachineArn"`
	Status          []string `json:"status"`
}

func (b builder) events(machine *sfn.StateMachine) (*sqs.Queue, error) {
	a, s := b.args, b.args.Settings
	pattern := pulumi.JSONMarshal(pulumix.Apply2(a.Base.BucketName, a.Base.Objects.Output.ToStringOutput(), func(bucket, prefix string) objectPattern {
		return objectPattern{[]string{"aws.s3"}, []string{"Object Created"}, objectDetail{bucketPattern{[]string{bucket}}, keyPattern{[]wildcard{{prefix + "/*/vocals.wav"}}}}}
	}))
	created, err := cloudwatch.NewEventRule(b.ctx, b.name+"-vocals-created", &cloudwatch.EventRuleArgs{EventPattern: pattern, Tags: a.Tags}, b.parent)
	if err != nil {
		return nil, err
	}
	failedPattern := pulumi.JSONMarshal(pulumix.Apply(machine.Arn, func(arn string) failurePattern {
		return failurePattern{[]string{"aws.states"}, []string{"Step Functions Execution Status Change"}, failureDetail{[]string{arn}, []string{"FAILED", "TIMED_OUT", "ABORTED"}}}
	}))
	failed, err := cloudwatch.NewEventRule(b.ctx, b.name+"-workflow-failed", &cloudwatch.EventRuleArgs{EventPattern: failedPattern, Tags: a.Tags}, b.parent)
	if err != nil {
		return nil, err
	}
	queue, err := sqs.NewQueue(b.ctx, b.name+"-errors", &sqs.QueueArgs{MessageRetentionSeconds: pulumi.Int(s.DeadLetterRetentionDays * 86400), SqsManagedSseEnabled: pulumi.Bool(true), Tags: a.Tags}, b.parent)
	if err != nil {
		return nil, err
	}
	send := allow([]string{"sqs:SendMessage"}, queue.Arn)
	send.Principals = iam.GetPolicyDocumentStatementPrincipalArray{iam.GetPolicyDocumentStatementPrincipalArgs{Type: pulumi.String("Service"), Identifiers: pulumi.StringArray{pulumi.String("events.amazonaws.com")}}}
	send.Conditions = iam.GetPolicyDocumentStatementConditionArray{iam.GetPolicyDocumentStatementConditionArgs{Test: pulumi.String("ArnEquals"), Variable: pulumi.String("aws:SourceArn"), Values: pulumi.StringArray{created.Arn, failed.Arn}}}
	policy, err := sqs.NewQueuePolicy(b.ctx, b.name+"-errors-policy", &sqs.QueuePolicyArgs{QueueUrl: queue.Url, Policy: document(b.ctx, send)}, b.parent)
	if err != nil {
		return nil, err
	}
	role, err := b.role("event-role", "events.amazonaws.com", created.Arn)
	if err != nil {
		return nil, err
	}
	invoke, err := b.policy("event-policy", role, allow([]string{"states:StartExecution"}, machine.Arn))
	if err != nil {
		return nil, err
	}
	retry := &cloudwatch.EventTargetRetryPolicyArgs{MaximumEventAgeInSeconds: pulumi.Int(s.EventMaxAgeSeconds), MaximumRetryAttempts: pulumi.Int(s.EventRetryAttempts)}
	if _, err = cloudwatch.NewEventTarget(b.ctx, b.name+"-start", &cloudwatch.EventTargetArgs{Rule: created.Name, Arn: machine.Arn, RoleArn: role.ARN, RetryPolicy: retry, DeadLetterConfig: &cloudwatch.EventTargetDeadLetterConfigArgs{Arn: queue.Arn}}, b.parent, pulumi.DependsOn([]pulumi.Resource{policy, invoke})); err != nil {
		return nil, err
	}
	// Failed workflow events are ordinary messages; the same queue also captures delivery failures.
	if _, err = cloudwatch.NewEventTarget(b.ctx, b.name+"-failure", &cloudwatch.EventTargetArgs{Rule: failed.Name, Arn: queue.Arn, RetryPolicy: retry, DeadLetterConfig: &cloudwatch.EventTargetDeadLetterConfigArgs{Arn: queue.Arn}}, b.parent, pulumi.DependsOn([]pulumi.Resource{policy})); err != nil {
		return nil, err
	}
	return queue, nil
}
func (b builder) alarms(machine *sfn.StateMachine, prepare, finalize *lambda.Function, queue *sqs.Queue) error {
	type metric struct {
		name, namespace, metric string
		dimensions              pulumi.StringMap
	}
	metrics := []metric{
		{"workflow-failed", "AWS/States", "ExecutionsFailed", pulumi.StringMap{"StateMachineArn": machine.Arn}},
		{"workflow-timeout", "AWS/States", "ExecutionsTimedOut", pulumi.StringMap{"StateMachineArn": machine.Arn}},
		{"workflow-aborted", "AWS/States", "ExecutionsAborted", pulumi.StringMap{"StateMachineArn": machine.Arn}},
		{"prepare-errors", "AWS/Lambda", "Errors", pulumi.StringMap{"FunctionName": prepare.Name}},
		{"finalize-errors", "AWS/Lambda", "Errors", pulumi.StringMap{"FunctionName": finalize.Name}},
		{"queued-errors", "AWS/SQS", "ApproximateNumberOfMessagesVisible", pulumi.StringMap{"QueueName": queue.Name}},
	}
	for _, m := range metrics {
		if _, err := cloudwatch.NewMetricAlarm(b.ctx, b.name+"-"+m.name, &cloudwatch.MetricAlarmArgs{Namespace: pulumi.String(m.namespace), MetricName: pulumi.String(m.metric), Dimensions: m.dimensions, Statistic: pulumi.String("Sum"), Period: pulumi.Int(60), EvaluationPeriods: pulumi.Int(1), Threshold: pulumi.Float64(0), ComparisonOperator: pulumi.String("GreaterThanThreshold"), TreatMissingData: pulumi.String("notBreaching"), Tags: b.args.Tags}, b.parent); err != nil {
			return err
		}
	}
	return nil
}
