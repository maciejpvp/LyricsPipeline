package autoscaling

import (
	"fmt"
	"time"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Missing ECS counts are expected at zero capacity. An explicitly reported
// in-flight zero is required to wake; all ECS readiness counts are required to expand.
func demandExpression(backlog int) string {
	const zeroCapacity = "(FILL(desired, 0) == 0) AND (FILL(running, 0) == 0) AND (FILL(pending, 0) == 0)"
	const noInFlight = "(FILL(in_flight, 1) == 0)"
	const readyCapacity = "(FILL(running, 0) > 0) AND (FILL(desired, 0) == FILL(running, 0)) AND (FILL(pending, 1) == 0)"
	wakeDemand := fmt.Sprintf("IF(FILL(visible, 0) > 0, %d, 0)", backlog)
	readyDemand := "IF(" + readyCapacity + ", FILL(visible, 0) / running, 0)"
	return "IF(" + zeroCapacity + " AND " + noInFlight + ", " + wakeDemand + ", " + readyDemand + ")"
}

// Arithmetic treats missing counts as zero; fill each missing input with one
// so that only two explicitly reported zeros count as an idle minute.
const idleExpression = "FILL(visible, 1) + FILL(in_flight, 1)"

type metricDefinition struct{ id, namespace, name, statistic string }

func (m metricDefinition) query(dimensions pulumi.StringMap) cloudwatch.MetricAlarmMetricQueryArgs {
	return cloudwatch.MetricAlarmMetricQueryArgs{
		Id:         pulumi.String(m.id),
		ReturnData: pulumi.Bool(false),
		Metric: cloudwatch.MetricAlarmMetricQueryMetricArgs{
			Namespace:  pulumi.String(m.namespace),
			MetricName: pulumi.String(m.name),
			Stat:       pulumi.String(m.statistic),
			Dimensions: dimensions,
			Period:     pulumi.Int(metricPeriod / time.Second),
		},
	}
}

func (b *builder) queueQueries() cloudwatch.MetricAlarmMetricQueryArray {
	dimensions := pulumi.StringMap{"QueueName": b.args.QueueName}
	visible := metricDefinition{
		id:        "visible",
		namespace: "AWS/SQS",
		name:      "ApproximateNumberOfMessagesVisible",
		statistic: "Maximum",
	}
	inFlight := metricDefinition{
		id:        "in_flight",
		namespace: "AWS/SQS",
		name:      "ApproximateNumberOfMessagesNotVisible",
		statistic: "Maximum",
	}
	return cloudwatch.MetricAlarmMetricQueryArray{visible.query(dimensions), inFlight.query(dimensions)}
}

func (b *builder) serviceQueries() cloudwatch.MetricAlarmMetricQueryArray {
	dimensions := pulumi.StringMap{"ClusterName": b.args.ClusterName, "ServiceName": b.args.ServiceName}
	desired := metricDefinition{
		id:        "desired",
		namespace: "ECS/ContainerInsights",
		name:      "DesiredTaskCount",
		statistic: "Maximum",
	}
	// Minimum running capacity prevents deployment transitions from looking ready.
	running := metricDefinition{
		id:        "running",
		namespace: "ECS/ContainerInsights",
		name:      "RunningTaskCount",
		statistic: "Minimum",
	}
	pending := metricDefinition{
		id:        "pending",
		namespace: "ECS/ContainerInsights",
		name:      "PendingTaskCount",
		statistic: "Maximum",
	}
	return cloudwatch.MetricAlarmMetricQueryArray{desired.query(dimensions), running.query(dimensions), pending.query(dimensions)}
}
