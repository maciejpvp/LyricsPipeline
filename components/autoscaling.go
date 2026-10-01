package components

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/appautoscaling"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func NewAutoscaling(ctx *pulumi.Context, name string, maxTasks int, worker *Worker, queueName pulumi.StringInput) error {
	resourceID := pulumi.Sprintf("service/%s/%s", worker.Cluster.Name, worker.Service.Name)
	if _, err := appautoscaling.NewTarget(ctx, name+"-scaling-target", &appautoscaling.TargetArgs{ServiceNamespace: pulumi.String("ecs"), ScalableDimension: pulumi.String("ecs:service:DesiredCount"), ResourceId: resourceID, MinCapacity: pulumi.Int(0), MaxCapacity: pulumi.Int(maxTasks)}); err != nil {
		return err
	}
	scaleOut, err := appautoscaling.NewPolicy(ctx, name+"-scale-out", &appautoscaling.PolicyArgs{PolicyType: pulumi.String("StepScaling"), ServiceNamespace: pulumi.String("ecs"), ScalableDimension: pulumi.String("ecs:service:DesiredCount"), ResourceId: resourceID, StepScalingPolicyConfiguration: appautoscaling.PolicyStepScalingPolicyConfigurationArgs{AdjustmentType: pulumi.String("ChangeInCapacity"), Cooldown: pulumi.Int(60), StepAdjustments: appautoscaling.PolicyStepScalingPolicyConfigurationStepAdjustmentArray{appautoscaling.PolicyStepScalingPolicyConfigurationStepAdjustmentArgs{MetricIntervalLowerBound: pulumi.String("0"), ScalingAdjustment: pulumi.Int(1)}}}})
	if err != nil {
		return err
	}
	scaleIn, err := appautoscaling.NewPolicy(ctx, name+"-scale-in", &appautoscaling.PolicyArgs{PolicyType: pulumi.String("StepScaling"), ServiceNamespace: pulumi.String("ecs"), ScalableDimension: pulumi.String("ecs:service:DesiredCount"), ResourceId: resourceID, StepScalingPolicyConfiguration: appautoscaling.PolicyStepScalingPolicyConfigurationArgs{AdjustmentType: pulumi.String("ExactCapacity"), Cooldown: pulumi.Int(120), StepAdjustments: appautoscaling.PolicyStepScalingPolicyConfigurationStepAdjustmentArray{appautoscaling.PolicyStepScalingPolicyConfigurationStepAdjustmentArgs{MetricIntervalUpperBound: pulumi.String("0"), ScalingAdjustment: pulumi.Int(0)}}}})
	if err != nil {
		return err
	}
	for _, threshold := range []float64{0, 5, 20, 50} {
		comparison := "GreaterThanThreshold"
		alarmName := fmt.Sprintf("%s-messages-over-%d", name, int(threshold))
		actions := pulumi.Array{scaleOut.Arn}
		if threshold == 0 {
			alarmName = name + "-messages-visible"
		}
		if _, err = cloudwatch.NewMetricAlarm(ctx, alarmName, &cloudwatch.MetricAlarmArgs{Namespace: pulumi.String("AWS/SQS"), MetricName: pulumi.String("ApproximateNumberOfMessagesVisible"), Dimensions: pulumi.StringMap{"QueueName": queueName}, Statistic: pulumi.String("Maximum"), Period: pulumi.Int(60), EvaluationPeriods: pulumi.Int(1), ComparisonOperator: pulumi.String(comparison), Threshold: pulumi.Float64(threshold), AlarmActions: actions, TreatMissingData: pulumi.String("notBreaching")}); err != nil {
			return err
		}
	}
	if _, err = cloudwatch.NewMetricAlarm(ctx, name+"-messages-empty", &cloudwatch.MetricAlarmArgs{Namespace: pulumi.String("AWS/SQS"), MetricName: pulumi.String("ApproximateNumberOfMessagesVisible"), Dimensions: pulumi.StringMap{"QueueName": queueName}, Statistic: pulumi.String("Maximum"), Period: pulumi.Int(60), EvaluationPeriods: pulumi.Int(2), ComparisonOperator: pulumi.String("LessThanOrEqualToThreshold"), Threshold: pulumi.Float64(0), AlarmActions: pulumi.Array{scaleIn.Arn}, TreatMissingData: pulumi.String("notBreaching")}); err != nil {
		return err
	}
	return nil
}
