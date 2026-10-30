package components

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/appautoscaling"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Autoscaling struct {
	pulumi.ResourceState
	Target   *appautoscaling.Target
	ScaleOut *appautoscaling.Policy
	ScaleIn  *appautoscaling.Policy
}

type AutoscalingArgs struct {
	MinTasks                int
	MaxTasks                int
	IdleMinutes             int
	BacklogPerWorker        int
	ScaleOutCooldownSeconds int
	Worker                  *Worker
	QueueName               pulumi.StringInput
}

func NewAutoscaling(ctx *pulumi.Context, name string, args *AutoscalingArgs, opts ...pulumi.ResourceOption) (*Autoscaling, error) {
	component := &Autoscaling{}
	if err := ctx.RegisterComponentResource("lyrics:autoscaling:Autoscaling", name, component, opts...); err != nil {
		return nil, err
	}
	parent := pulumi.Parent(component)
	resourceID := pulumi.Sprintf("service/%s/%s", args.Worker.Cluster.Name, args.Worker.Service.Name)
	queueName := args.QueueName
	targetArgs := &appautoscaling.TargetArgs{
		ServiceNamespace:  pulumi.String("ecs"),
		ScalableDimension: pulumi.String("ecs:service:DesiredCount"),
		ResourceId:        resourceID,
		MinCapacity:       pulumi.Int(args.MinTasks),
		MaxCapacity:       pulumi.Int(args.MaxTasks),
	}

	target, err := appautoscaling.NewTarget(
		ctx,
		name+"-scaling-target",
		targetArgs,
		parent,
		pulumi.DependsOn([]pulumi.Resource{args.Worker.Service}),
	)
	if err != nil {
		return nil, err
	}
	scaleOut, err := appautoscaling.NewPolicy(
		ctx,
		name+"-scale-out",
		buildStepScalingPolicy(
			resourceID,
			"ChangeInCapacity",
			args.ScaleOutCooldownSeconds,
			appautoscaling.PolicyStepScalingPolicyConfigurationStepAdjustmentArgs{
				MetricIntervalLowerBound: pulumi.String("0"),
				ScalingAdjustment:        pulumi.Int(1),
			},
		),
		parent,
		pulumi.DependsOn([]pulumi.Resource{target}),
	)
	if err != nil {
		return nil, err
	}
	scaleIn, err := appautoscaling.NewPolicy(
		ctx,
		name+"-scale-in",
		buildStepScalingPolicy(
			resourceID,
			"ExactCapacity",
			120,
			appautoscaling.PolicyStepScalingPolicyConfigurationStepAdjustmentArgs{
				MetricIntervalUpperBound: pulumi.String("0"),
				ScalingAdjustment:        pulumi.Int(args.MinTasks),
			},
		),
		parent,
		pulumi.DependsOn([]pulumi.Resource{target}),
	)
	if err != nil {
		return nil, err
	}
	// Missing ECS counts are expected at zero capacity. Require an explicit
	// in-flight zero before waking; missing SQS counts must not invent work.
	// Once any capacity is reported, all ECS counts must report readiness.
	scaleOutExpression := fmt.Sprintf(
		"IF((FILL(desired, 0) == 0) AND (FILL(running, 0) == 0) AND (FILL(pending, 0) == 0) AND (FILL(in_flight, 1) == 0), IF(FILL(visible, 0) > 0, %d, 0), IF((FILL(running, 0) > 0) AND (FILL(desired, 0) == FILL(running, 0)) AND (FILL(pending, 1) == 0), FILL(visible, 0) / running, 0))",
		args.BacklogPerWorker,
	)
	queueDimensions := pulumi.StringMap{"QueueName": queueName}
	serviceDimensions := pulumi.StringMap{
		"ClusterName": args.Worker.Cluster.Name,
		"ServiceName": args.Worker.Service.Name,
	}
	if _, err = cloudwatch.NewMetricAlarm(ctx, name+"-messages-visible", &cloudwatch.MetricAlarmArgs{
		MetricQueries: cloudwatch.MetricAlarmMetricQueryArray{
			scalingMetric("visible", "AWS/SQS", "ApproximateNumberOfMessagesVisible", "Maximum", queueDimensions),
			scalingMetric("in_flight", "AWS/SQS", "ApproximateNumberOfMessagesNotVisible", "Maximum", queueDimensions),
			scalingMetric("desired", "ECS/ContainerInsights", "DesiredTaskCount", "Maximum", serviceDimensions),
			scalingMetric("running", "ECS/ContainerInsights", "RunningTaskCount", "Minimum", serviceDimensions),
			scalingMetric("pending", "ECS/ContainerInsights", "PendingTaskCount", "Maximum", serviceDimensions),
			cloudwatch.MetricAlarmMetricQueryArgs{
				Id:         pulumi.String("backlog_per_worker"),
				Expression: pulumi.String(scaleOutExpression),
				Label:      pulumi.String("Waiting messages per ready worker, or wake-up demand"),
				ReturnData: pulumi.Bool(true),
			},
		},
		EvaluationPeriods:  pulumi.Int(1),
		DatapointsToAlarm:  pulumi.Int(1),
		ComparisonOperator: pulumi.String("GreaterThanOrEqualToThreshold"),
		Threshold:          pulumi.Float64(args.BacklogPerWorker),
		AlarmActions:       pulumi.Array{scaleOut.Arn},
		TreatMissingData:   pulumi.String("notBreaching"),
		AlarmDescription:   pulumi.StringPtr("Wake from zero or add one worker when waiting backlog per ready worker reaches the threshold; wait for desired capacity to be running"),
	}, parent); err != nil {
		return nil, err
	}
	emptyAlarmArgs := &cloudwatch.MetricAlarmArgs{
		MetricQueries: cloudwatch.MetricAlarmMetricQueryArray{
			cloudwatch.MetricAlarmMetricQueryArgs{
				Id: pulumi.String("visible"),
				Metric: cloudwatch.MetricAlarmMetricQueryMetricArgs{
					Namespace:  pulumi.String("AWS/SQS"),
					MetricName: pulumi.String("ApproximateNumberOfMessagesVisible"),
					Dimensions: pulumi.StringMap{"QueueName": queueName},
					Stat:       pulumi.String("Maximum"),
					Period:     pulumi.Int(60),
				},
				ReturnData: pulumi.Bool(false),
			},
			cloudwatch.MetricAlarmMetricQueryArgs{
				Id: pulumi.String("in_flight"),
				Metric: cloudwatch.MetricAlarmMetricQueryMetricArgs{
					Namespace:  pulumi.String("AWS/SQS"),
					MetricName: pulumi.String("ApproximateNumberOfMessagesNotVisible"),
					Dimensions: pulumi.StringMap{"QueueName": queueName},
					Stat:       pulumi.String("Maximum"),
					Period:     pulumi.Int(60),
				},
				ReturnData: pulumi.Bool(false),
			},
			cloudwatch.MetricAlarmMetricQueryArgs{
				Id: pulumi.String("total"),
				// Arithmetic alone treats missing inputs as zero. Fill each
				// missing count with one so only two reported zeros mean idle.
				Expression: pulumi.String("FILL(visible, 1) + FILL(in_flight, 1)"),
				Label:      pulumi.String("Visible and in-flight messages"),
				ReturnData: pulumi.Bool(true),
			},
		},
		// Every minute must report both counts as zero.
		EvaluationPeriods:  pulumi.Int(args.IdleMinutes),
		DatapointsToAlarm:  pulumi.Int(args.IdleMinutes),
		ComparisonOperator: pulumi.String("LessThanOrEqualToThreshold"),
		Threshold:          pulumi.Float64(0),
		AlarmActions:       pulumi.Array{scaleIn.Arn},
		TreatMissingData:   pulumi.String("notBreaching"),
		AlarmDescription:   pulumi.StringPtr("Scale in only when no SQS messages are visible or in flight"),
	}

	if _, err = cloudwatch.NewMetricAlarm(ctx, name+"-messages-empty", emptyAlarmArgs, parent); err != nil {
		return nil, err
	}
	component.Target = target
	component.ScaleOut = scaleOut
	component.ScaleIn = scaleIn
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"scalingTargetId":   target.ID(),
		"scaleOutPolicyArn": scaleOut.Arn,
		"scaleInPolicyArn":  scaleIn.Arn,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

func scalingMetric(id, namespace, metricName, statistic string, dimensions pulumi.StringMap) cloudwatch.MetricAlarmMetricQueryArgs {
	return cloudwatch.MetricAlarmMetricQueryArgs{
		Id: pulumi.String(id),
		Metric: cloudwatch.MetricAlarmMetricQueryMetricArgs{
			Namespace:  pulumi.String(namespace),
			MetricName: pulumi.String(metricName),
			Dimensions: dimensions,
			Stat:       pulumi.String(statistic),
			Period:     pulumi.Int(60),
		},
		ReturnData: pulumi.Bool(false),
	}
}

func buildStepScalingPolicy(
	resourceID pulumi.StringOutput,
	adjustmentType string,
	cooldown int,
	stepAdjustment appautoscaling.PolicyStepScalingPolicyConfigurationStepAdjustmentArgs,
) *appautoscaling.PolicyArgs {
	return &appautoscaling.PolicyArgs{
		PolicyType:        pulumi.String("StepScaling"),
		ServiceNamespace:  pulumi.String("ecs"),
		ScalableDimension: pulumi.String("ecs:service:DesiredCount"),
		ResourceId:        resourceID,
		StepScalingPolicyConfiguration: appautoscaling.PolicyStepScalingPolicyConfigurationArgs{
			AdjustmentType: pulumi.String(adjustmentType),
			Cooldown:       pulumi.Int(cooldown),
			StepAdjustments: appautoscaling.PolicyStepScalingPolicyConfigurationStepAdjustmentArray{
				stepAdjustment,
			},
		},
	}
}
