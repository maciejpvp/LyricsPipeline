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

func NewAutoscaling(ctx *pulumi.Context, name string, maxTasks int, worker *Worker, queueName pulumi.StringInput, opts ...pulumi.ResourceOption) (*Autoscaling, error) {
	component := &Autoscaling{}
	if err := ctx.RegisterComponentResource("lyrics:autoscaling:Autoscaling", name, component, opts...); err != nil {
		return nil, err
	}
	parent := pulumi.Parent(component)
	resourceID := pulumi.Sprintf("service/%s/%s", worker.Cluster.Name, worker.Service.Name)
	targetArgs := &appautoscaling.TargetArgs{
		ServiceNamespace:  pulumi.String("ecs"),
		ScalableDimension: pulumi.String("ecs:service:DesiredCount"),
		ResourceId:        resourceID,
		MinCapacity:       pulumi.Int(0),
		MaxCapacity:       pulumi.Int(maxTasks),
	}

	target, err := appautoscaling.NewTarget(
		ctx,
		name+"-scaling-target",
		targetArgs,
		parent,
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
			60,
			appautoscaling.PolicyStepScalingPolicyConfigurationStepAdjustmentArgs{
				MetricIntervalLowerBound: pulumi.String("0"),
				ScalingAdjustment:        pulumi.Int(1),
			},
		),
		parent,
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
				ScalingAdjustment:        pulumi.Int(0),
			},
		),
		parent,
	)
	if err != nil {
		return nil, err
	}
	for _, threshold := range []float64{0, 5, 20, 50} {
		comparison := "GreaterThanThreshold"
		alarmName := fmt.Sprintf("%s-messages-over-%d", name, int(threshold))
		actions := pulumi.Array{scaleOut.Arn}
		if threshold == 0 {
			alarmName = name + "-messages-visible"
		}
		alarmArgs := &cloudwatch.MetricAlarmArgs{
			Namespace:          pulumi.String("AWS/SQS"),
			MetricName:         pulumi.String("ApproximateNumberOfMessagesVisible"),
			Dimensions:         pulumi.StringMap{"QueueName": queueName},
			Statistic:          pulumi.String("Maximum"),
			Period:             pulumi.Int(60),
			EvaluationPeriods:  pulumi.Int(1),
			ComparisonOperator: pulumi.String(comparison),
			Threshold:          pulumi.Float64(threshold),
			AlarmActions:       actions,
			TreatMissingData:   pulumi.String("notBreaching"),
		}

		if _, err = cloudwatch.NewMetricAlarm(ctx, alarmName, alarmArgs, parent); err != nil {
			return nil, err
		}
	}
	emptyAlarmArgs := &cloudwatch.MetricAlarmArgs{
		Namespace:          pulumi.String("AWS/SQS"),
		MetricName:         pulumi.String("ApproximateNumberOfMessagesVisible"),
		Dimensions:         pulumi.StringMap{"QueueName": queueName},
		Statistic:          pulumi.String("Maximum"),
		Period:             pulumi.Int(60),
		EvaluationPeriods:  pulumi.Int(2),
		ComparisonOperator: pulumi.String("LessThanOrEqualToThreshold"),
		Threshold:          pulumi.Float64(0),
		AlarmActions:       pulumi.Array{scaleIn.Arn},
		TreatMissingData:   pulumi.String("notBreaching"),
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
