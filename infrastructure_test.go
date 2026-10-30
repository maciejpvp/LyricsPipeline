package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"LyricsPipeline/internal/deployment"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type infraMocks struct {
	sync.Mutex
	resources       map[string]pulumi.MockResourceArgs
	allResources    map[string]pulumi.MockResourceArgs
	calls           []pulumi.MockCallArgs
	providerRegion  string
	zoneRegion      string
	previewUnknowns bool
}

func (m *infraMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.Lock()
	defer m.Unlock()
	if m.resources == nil {
		m.resources = map[string]pulumi.MockResourceArgs{}
	}
	m.resources[args.Name] = args
	if m.allResources == nil {
		m.allResources = map[string]pulumi.MockResourceArgs{}
	}
	m.allResources[args.TypeToken+"::"+args.Name] = args
	outputs := args.Inputs.Copy()
	for key, value := range map[resource.PropertyKey]string{
		"name": args.Name, "arn": "arn:aws:mock::123456789012:" + args.Name,
		"bucket": args.Name, "url": "https://sqs.example/" + args.Name,
		"repositoryUrl": "registry.example/" + args.Name,
	} {
		if _, ok := outputs[key]; !ok {
			outputs[key] = resource.NewStringProperty(value)
			if m.previewUnknowns && args.Custom {
				outputs[key] = resource.MakeComputed(outputs[key])
			}
		}
	}
	return args.Name + "-id", outputs, nil
}

func TestSpotAutoscaling(t *testing.T) {
	for _, tc := range []struct{ min, max, idle, backlog, cooldown int }{{0, 2, 5, 5, 300}, {1, 2, 3, 5, 300}, {0, 10, 5, 3, 600}} {
		t.Run(fmt.Sprintf("min%d_max%d_backlog%d", tc.min, tc.max, tc.backlog), func(t *testing.T) {
			mocks := &infraMocks{}
			err := pulumi.RunErr(deployment.Run,
				pulumi.WithMocks("LyricsPipeline", "test", mocks),
				withDeployment(fmt.Sprintf(`{"minTasks":%d,"maxTasks":%d,"idleMinutes":%d,"backlogPerWorker":%d,"scaleOutCooldownSeconds":%d}`, tc.min, tc.max, tc.idle, tc.backlog, tc.cooldown)))
			if err != nil {
				t.Fatal(err)
			}
			get := func(suffix string) pulumi.MockResourceArgs {
				t.Helper()
				r, ok := mocks.resources["vocal-extractor-"+suffix]
				if !ok {
					t.Fatalf("missing resource %s", suffix)
				}
				return r
			}
			inputs := func(suffix string) map[string]interface{} { return get(suffix).Inputs.Mappable() }
			equal := func(got, want interface{}) {
				t.Helper()
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got %#v, want %#v", got, want)
				}
			}
			depends := func(child, parent string) {
				t.Helper()
				for _, urn := range get(child).RegisterRPC.GetDependencies() {
					if resource.URN(urn).Name() == "vocal-extractor-"+parent {
						return
					}
				}
				t.Errorf("%s must depend on %s", child, parent)
			}
			equal(inputs("capacity-providers")["capacityProviders"], []interface{}{"FARGATE_SPOT"})
			equal(inputs("cluster")["settings"], []interface{}{map[string]interface{}{
				"name": "containerInsights", "value": "enabled",
			}})
			service := inputs("service")
			if _, ok := service["launchType"]; ok {
				t.Error("Spot service must not specify launchType")
			}
			equal(service["capacityProviderStrategies"], []interface{}{map[string]interface{}{
				"capacityProvider": "FARGATE_SPOT", "weight": float64(1), "base": float64(0),
			}})
			equal(service["desiredCount"], float64(tc.min))
			equal(service["forceNewDeployment"], true)
			equal(service["triggers"], map[string]interface{}{"capacityProvider": "FARGATE_SPOT"})
			if !slices.Contains(get("service").RegisterRPC.GetIgnoreChanges(), "desiredCount") {
				t.Error("desiredCount must belong to autoscaler")
			}
			depends("service", "capacity-providers")
			depends("scaling-target", "service")
			depends("scale-out", "scaling-target")
			depends("scale-in", "scaling-target")
			equal(inputs("scaling-target")["minCapacity"], float64(tc.min))
			equal(inputs("scaling-target")["maxCapacity"], float64(tc.max))
			for _, policy := range []struct {
				name, adjustment string
				value, cooldown  int
			}{
				{"scale-out", "ChangeInCapacity", 1, tc.cooldown}, {"scale-in", "ExactCapacity", tc.min, 120},
			} {
				cfg := inputs(policy.name)["stepScalingPolicyConfiguration"].(map[string]interface{})
				equal(cfg["adjustmentType"], policy.adjustment)
				equal(cfg["cooldown"], float64(policy.cooldown))
				steps := cfg["stepAdjustments"].([]interface{})
				equal(steps[0].(map[string]interface{})["scalingAdjustment"], float64(policy.value))
			}
			alarms := 0
			for _, r := range mocks.resources {
				if r.TypeToken == "aws:cloudwatch/metricAlarm:MetricAlarm" {
					alarms++
				}
				if strings.Contains(r.TypeToken, "lambda/") || strings.Contains(r.TypeToken, "dynamodb/") || strings.Contains(r.TypeToken, "scheduler/") || strings.Contains(r.TypeToken, "cloudwatch/event") {
					t.Errorf("scaling must use native alarms/policies, found %s", r.TypeToken)
				}
			}
			equal(alarms, 2)
			out := inputs("messages-visible")
			for _, field := range []string{"metricName", "namespace", "statistic", "period"} {
				if _, ok := out[field]; ok {
					t.Errorf("metric math alarm must not retain top-level %s", field)
				}
			}
			equal(out["evaluationPeriods"], float64(1))
			equal(out["datapointsToAlarm"], float64(1))
			equal(out["threshold"], float64(tc.backlog))
			equal(out["comparisonOperator"], "GreaterThanOrEqualToThreshold")
			equal(out["treatMissingData"], "notBreaching")
			equal(out["alarmActions"], []interface{}{"arn:aws:mock::123456789012:vocal-extractor-scale-out"})
			outQueries := out["metricQueries"].([]interface{})
			equal(len(outQueries), 6)
			for i, metricName := range []string{"ApproximateNumberOfMessagesVisible", "ApproximateNumberOfMessagesNotVisible", "DesiredTaskCount", "RunningTaskCount", "PendingTaskCount"} {
				query := outQueries[i].(map[string]interface{})
				metric := query["metric"].(map[string]interface{})
				equal(query["returnData"], false)
				equal(metric["metricName"], metricName)
				equal(metric["period"], float64(60))
				stat := "Maximum"
				if i == 3 {
					stat = "Minimum"
				}
				equal(metric["stat"], stat)
				if i < 2 {
					equal(metric["namespace"], "AWS/SQS")
					equal(metric["dimensions"], map[string]interface{}{"QueueName": "vocal-extractor-queue"})
				} else {
					equal(metric["namespace"], "ECS/ContainerInsights")
					equal(metric["dimensions"], map[string]interface{}{"ClusterName": "vocal-extractor-cluster", "ServiceName": "vocal-extractor-service"})
				}
			}
			outExpression := outQueries[5].(map[string]interface{})
			equal(outExpression["returnData"], true)
			testScaleOutExpression(t, outExpression["expression"].(string), tc.backlog)
			in := inputs("messages-empty")
			equal(in["evaluationPeriods"], float64(tc.idle))
			equal(in["datapointsToAlarm"], float64(tc.idle))
			equal(in["threshold"], float64(0))
			equal(in["comparisonOperator"], "LessThanOrEqualToThreshold")
			equal(in["treatMissingData"], "notBreaching")
			equal(in["alarmActions"], []interface{}{"arn:aws:mock::123456789012:vocal-extractor-scale-in"})
			queries := in["metricQueries"].([]interface{})
			for i, metricName := range []string{"ApproximateNumberOfMessagesVisible", "ApproximateNumberOfMessagesNotVisible"} {
				metric := queries[i].(map[string]interface{})["metric"].(map[string]interface{})
				equal(metric["metricName"], metricName)
				equal(metric["namespace"], "AWS/SQS")
				equal(metric["period"], float64(60))
				equal(metric["stat"], "Maximum")
				equal(metric["dimensions"], map[string]interface{}{"QueueName": "vocal-extractor-queue"})
			}
			expression := queries[2].(map[string]interface{})["expression"].(string)
			testIdleExpression(t, expression, tc.idle)
			equal(inputs("task")["cpu"], "2048")
			equal(inputs("task")["memory"], "8192")
			var containers []map[string]interface{}
			if err := json.Unmarshal([]byte(inputs("task")["containerDefinitions"].(string)), &containers); err != nil {
				t.Fatal(err)
			}
			equal(containers[0]["stopTimeout"], float64(120))
			var redrive map[string]interface{}
			if err := json.Unmarshal([]byte(inputs("queue")["redrivePolicy"].(string)), &redrive); err != nil {
				t.Fatal(err)
			}
			equal(redrive["maxReceiveCount"], float64(5))
		})
	}
}

// Evaluate the emitted CloudWatch arithmetic, comparison, IF and FILL subset.
// This checks policy decisions, not AWS publication, alarm or cooldown timing.
func metricEvaluator(t *testing.T, expression string) func(map[string]float64) float64 {
	t.Helper()
	expr, err := parser.ParseExpr(strings.ReplaceAll(expression, " AND ", " && "))
	if err != nil {
		t.Fatal(err)
	}
	var eval func(ast.Expr, map[string]float64) float64
	boolean := func(value bool) float64 {
		if value {
			return 1
		}
		return 0
	}
	eval = func(expr ast.Expr, sample map[string]float64) float64 {
		switch e := expr.(type) {
		case *ast.ParenExpr:
			return eval(e.X, sample)
		case *ast.Ident:
			return sample[e.Name] // CloudWatch arithmetic defaults missing to zero.
		case *ast.BasicLit:
			value, err := strconv.ParseFloat(e.Value, 64)
			if err != nil {
				t.Fatal(err)
			}
			return value
		case *ast.BinaryExpr:
			x, y := eval(e.X, sample), eval(e.Y, sample)
			switch e.Op {
			case token.ADD:
				return x + y
			case token.QUO:
				if y == 0 {
					return math.NaN() // CloudWatch drops division-by-zero points.
				}
				return x / y
			case token.EQL:
				return boolean(x == y)
			case token.GTR:
				return boolean(x > y)
			case token.LAND:
				return boolean(x != 0 && y != 0)
			}
		case *ast.CallExpr:
			if fn, ok := e.Fun.(*ast.Ident); ok && fn.Name == "FILL" && len(e.Args) == 2 {
				id := e.Args[0].(*ast.Ident).Name
				if value, ok := sample[id]; ok {
					return value
				}
				return eval(e.Args[1], sample)
			}
			if fn, ok := e.Fun.(*ast.Ident); ok && fn.Name == "IF" && len(e.Args) == 3 {
				if eval(e.Args[0], sample) != 0 {
					return eval(e.Args[1], sample)
				}
				return eval(e.Args[2], sample)
			}
		}
		t.Fatalf("unsupported metric math: %s", expression)
		return 0
	}
	return func(sample map[string]float64) float64 { return eval(expr, sample) }
}

func testScaleOutExpression(t *testing.T, expression string, backlog int) {
	t.Helper()
	eval := metricEvaluator(t, expression)
	ready := func(workers, visible float64) map[string]float64 {
		return map[string]float64{"visible": visible, "in_flight": workers, "desired": workers, "running": workers, "pending": 0}
	}
	threshold := float64(backlog)
	for _, tc := range []struct {
		name   string
		sample map[string]float64
		want   float64
	}{
		{"cold empty", map[string]float64{"visible": 0, "in_flight": 0}, 0},
		{"cold one upload", map[string]float64{"visible": 1, "in_flight": 0}, threshold},
		{"cold large burst still adds only one", map[string]float64{"visible": 100, "in_flight": 0}, threshold},
		{"reported zero capacity", map[string]float64{"visible": 1, "in_flight": 0, "desired": 0, "running": 0, "pending": 0}, threshold},
		{"desired before pending", map[string]float64{"visible": 100, "in_flight": 0, "desired": 1}, 0},
		{"first worker pending", map[string]float64{"visible": 100, "in_flight": 0, "desired": 1, "running": 0, "pending": 1}, 0},
		{"pending before desired metric", map[string]float64{"visible": 100, "in_flight": 0, "pending": 1}, 0},
		{"running before desired metric", map[string]float64{"visible": 100, "in_flight": 0, "running": 1, "pending": 0}, 0},
		{"in-flight before ECS metrics", map[string]float64{"visible": 1, "in_flight": 1}, 0},
		{"single active job", ready(1, 0), 0},
		{"one worker below threshold", ready(1, threshold-1), threshold - 1},
		{"one worker at threshold", ready(1, threshold), threshold},
		{"two workers below threshold", ready(2, 2*threshold-1), threshold - 0.5},
		{"two workers at threshold", ready(2, 2*threshold), threshold},
		{"three workers at threshold", ready(3, 3*threshold), threshold},
		{"large backlog remains breaching", ready(2, 100), 50},
		{"second worker pending", map[string]float64{"visible": 100, "in_flight": 1, "desired": 2, "running": 1, "pending": 1}, 0},
		{"desired leads pending metric", map[string]float64{"visible": 100, "in_flight": 1, "desired": 2, "running": 1, "pending": 0}, 0},
		{"rolling deployment overlap", map[string]float64{"visible": 100, "in_flight": 1, "desired": 1, "running": 2, "pending": 0}, 0},
		{"missing all metrics", map[string]float64{}, 0},
		{"missing cold visible", map[string]float64{"in_flight": 0}, 0},
		{"missing cold in-flight", map[string]float64{"visible": 100}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := eval(tc.sample)
			if got != tc.want {
				t.Fatalf("demand = %v, want %v, sample = %v", got, tc.want, tc.sample)
			}
		})
	}
	for _, missing := range []string{"visible", "desired", "running", "pending"} {
		t.Run("missing ready "+missing, func(t *testing.T) {
			sample := ready(1, 100)
			delete(sample, missing)
			if got := eval(sample); got != 0 {
				t.Fatalf("missing %s must block expansion, got %v", missing, got)
			}
		})
	}
	// One upload across bootstrap, start, processing and completion must never
	// request a second task. The shared AWS policy owns the actual cooldown.
	for i, sample := range []map[string]float64{
		{"visible": 1, "in_flight": 0},
		{"visible": 1, "in_flight": 0, "desired": 1},
		{"visible": 1, "in_flight": 0, "desired": 1, "running": 0, "pending": 1},
		ready(1, 1), // A stale visible count after RUNNING is still below threshold.
		ready(1, 0),
		{"visible": 0, "in_flight": 0, "desired": 1, "running": 1, "pending": 0},
	} {
		if i > 0 && eval(sample) >= threshold {
			t.Fatalf("single upload requests another worker at sample %d", i)
		}
	}
}

func testIdleExpression(t *testing.T, expression string, idle int) {
	t.Helper()
	eval := metricEvaluator(t, expression)
	empty := map[string]float64{"visible": 0, "in_flight": 0}
	for _, tc := range []struct {
		name   string
		sample map[string]float64
		idle   bool
	}{
		{"empty", empty, true},
		{"waiting", map[string]float64{"visible": 1, "in_flight": 0}, false},
		{"processing", map[string]float64{"visible": 0, "in_flight": 1}, false},
		{"missing visible", map[string]float64{"in_flight": 0}, false},
		{"missing in flight", map[string]float64{"visible": 0}, false},
		{"missing both", map[string]float64{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(tc.sample) <= 0; got != tc.idle {
				t.Fatalf("scale-in = %v, want %v", got, tc.idle)
			}
			// Any busy/missing minute resets the consecutive idle window.
			consecutive := 0
			for i := 0; i < idle; i++ {
				sample := empty
				if i == idle-1 {
					sample = tc.sample
				}
				if eval(sample) <= 0 {
					consecutive++
				} else {
					consecutive = 0
				}
			}
			if got := consecutive >= idle; got != tc.idle {
				t.Fatalf("idle window = %v, want %v", got, tc.idle)
			}
		})
	}
}

func withDeployment(raw string) pulumi.RunOption {
	return func(info *pulumi.RunInfo) { info.Config = map[string]string{"LyricsPipeline:deployment": raw} }
}
