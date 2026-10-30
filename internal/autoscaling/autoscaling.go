// Package autoscaling manages native ECS scaling policies and queue-demand alarms.
package autoscaling

import (
	"time"

	"LyricsPipeline/internal/config"

	awsappautoscaling "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/appautoscaling"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const metricPeriod = time.Minute
const serviceNamespace = "ecs"
const scalableDimension = "ecs:service:DesiredCount"

type Args struct {
	Settings     config.Scaling
	ClusterName  pulumi.StringInput
	ServiceName  pulumi.StringInput
	ServiceReady pulumi.Resource
	QueueName    pulumi.StringInput
}

type Autoscaling struct {
	pulumi.ResourceState
	TargetID    pulumi.IDOutput
	ScaleOutARN pulumi.StringOutput
	ScaleInARN  pulumi.StringOutput
}

type builder struct {
	ctx        *pulumi.Context
	name       string
	args       Args
	parent     pulumi.ResourceOption
	resourceID pulumi.StringOutput
}

func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Autoscaling, error) {
	component := &Autoscaling{}
	if err := ctx.RegisterComponentResource("lyrics:autoscaling:Autoscaling", name, component, opts...); err != nil {
		return nil, err
	}
	b := builder{
		ctx:        ctx,
		name:       name,
		args:       args,
		parent:     pulumi.Parent(component),
		resourceID: pulumi.Sprintf("service/%s/%s", args.ClusterName, args.ServiceName),
	}
	target, err := b.createTarget()
	if err != nil {
		return nil, err
	}
	scaleOut, scaleIn, err := b.createPolicies(target)
	if err != nil {
		return nil, err
	}
	if err := b.createScaleOutAlarm(scaleOut); err != nil {
		return nil, err
	}
	if err := b.createIdleAlarm(scaleIn); err != nil {
		return nil, err
	}
	component.TargetID, component.ScaleOutARN, component.ScaleInARN = target.ID(), scaleOut.Arn, scaleIn.Arn
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{"scalingTargetId": component.TargetID, "scaleOutPolicyArn": component.ScaleOutARN, "scaleInPolicyArn": component.ScaleInARN}); err != nil {
		return nil, err
	}
	return component, nil
}

func (b *builder) createTarget() (*awsappautoscaling.Target, error) {
	return awsappautoscaling.NewTarget(b.ctx, b.name+"-scaling-target", &awsappautoscaling.TargetArgs{
		ServiceNamespace:  pulumi.String(serviceNamespace),
		ScalableDimension: pulumi.String(scalableDimension),
		ResourceId:        b.resourceID,
		MinCapacity:       pulumi.Int(b.args.Settings.MinTasks),
		MaxCapacity:       pulumi.Int(b.args.Settings.MaxTasks),
	}, b.parent, pulumi.DependsOn([]pulumi.Resource{b.args.ServiceReady}))
}

func (b *builder) createPolicies(target *awsappautoscaling.Target) (*awsappautoscaling.Policy, *awsappautoscaling.Policy, error) {
	scaleOut, err := awsappautoscaling.NewPolicy(b.ctx, b.name+"-scale-out", b.policy(stepPolicy{
		adjustmentType: "ChangeInCapacity",
		cooldown:       b.args.Settings.ScaleOutCooldown,
		adjustment: awsappautoscaling.PolicyStepScalingPolicyConfigurationStepAdjustmentArgs{
			MetricIntervalLowerBound: pulumi.String("0"),
			ScalingAdjustment:        pulumi.Int(1),
		},
	}), b.parent, pulumi.DependsOn([]pulumi.Resource{target}))
	if err != nil {
		return nil, nil, err
	}
	scaleIn, err := awsappautoscaling.NewPolicy(b.ctx, b.name+"-scale-in", b.policy(stepPolicy{
		adjustmentType: "ExactCapacity",
		cooldown:       b.args.Settings.ScaleInCooldown,
		adjustment: awsappautoscaling.PolicyStepScalingPolicyConfigurationStepAdjustmentArgs{
			MetricIntervalUpperBound: pulumi.String("0"),
			ScalingAdjustment:        pulumi.Int(b.args.Settings.MinTasks),
		},
	}), b.parent, pulumi.DependsOn([]pulumi.Resource{target}))
	return scaleOut, scaleIn, err
}

type stepPolicy struct {
	adjustmentType string
	cooldown       time.Duration
	adjustment     awsappautoscaling.PolicyStepScalingPolicyConfigurationStepAdjustmentArgs
}

func (b *builder) policy(p stepPolicy) *awsappautoscaling.PolicyArgs {
	return &awsappautoscaling.PolicyArgs{
		PolicyType:        pulumi.String("StepScaling"),
		ServiceNamespace:  pulumi.String(serviceNamespace),
		ScalableDimension: pulumi.String(scalableDimension),
		ResourceId:        b.resourceID,
		StepScalingPolicyConfiguration: awsappautoscaling.PolicyStepScalingPolicyConfigurationArgs{
			AdjustmentType:  pulumi.String(p.adjustmentType),
			Cooldown:        pulumi.Int(p.cooldown / time.Second),
			StepAdjustments: awsappautoscaling.PolicyStepScalingPolicyConfigurationStepAdjustmentArray{p.adjustment},
		},
	}
}

func (b *builder) createScaleOutAlarm(policy *awsappautoscaling.Policy) error {
	queries := append(b.queueQueries(), b.serviceQueries()...)
	queries = append(queries, cloudwatch.MetricAlarmMetricQueryArgs{
		Id:         pulumi.String("backlog_per_worker"),
		Expression: pulumi.String(demandExpression(b.args.Settings.BacklogPerWorker)),
		Label:      pulumi.String("Waiting messages per ready worker, or wake-up demand"),
		ReturnData: pulumi.Bool(true),
	})
	_, err := cloudwatch.NewMetricAlarm(b.ctx, b.name+"-messages-visible", &cloudwatch.MetricAlarmArgs{
		MetricQueries:      queries,
		EvaluationPeriods:  pulumi.Int(1),
		DatapointsToAlarm:  pulumi.Int(1),
		ComparisonOperator: pulumi.String("GreaterThanOrEqualToThreshold"),
		Threshold:          pulumi.Float64(b.args.Settings.BacklogPerWorker),
		AlarmActions:       pulumi.Array{policy.Arn},
		TreatMissingData:   pulumi.String("notBreaching"),
		AlarmDescription:   pulumi.StringPtr("Wake from zero or add one worker when waiting backlog per ready worker reaches the threshold; wait for desired capacity to be running"),
	}, b.parent)
	return err
}

func (b *builder) createIdleAlarm(policy *awsappautoscaling.Policy) error {
	queries := append(b.queueQueries(), cloudwatch.MetricAlarmMetricQueryArgs{
		Id:         pulumi.String("total"),
		Expression: pulumi.String(idleExpression),
		Label:      pulumi.String("Visible and in-flight messages"),
		ReturnData: pulumi.Bool(true),
	})
	periods := pulumi.Int(b.args.Settings.IdleWindow / metricPeriod)
	_, err := cloudwatch.NewMetricAlarm(b.ctx, b.name+"-messages-empty", &cloudwatch.MetricAlarmArgs{
		MetricQueries:      queries,
		EvaluationPeriods:  periods,
		DatapointsToAlarm:  periods,
		ComparisonOperator: pulumi.String("LessThanOrEqualToThreshold"),
		Threshold:          pulumi.Float64(0),
		AlarmActions:       pulumi.Array{policy.Arn},
		TreatMissingData:   pulumi.String("notBreaching"),
		AlarmDescription:   pulumi.StringPtr("Scale in only when no SQS messages are visible or in flight"),
	}, b.parent)
	return err
}
