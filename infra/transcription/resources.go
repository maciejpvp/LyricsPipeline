package main

import (
	"fmt"

	"LyricsPipeline/infra/contracts"
	roles "LyricsPipeline/internal/iam"
	functions "LyricsPipeline/internal/lambda"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sfn"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumix"
)

type Args struct {
	Base            contracts.Base
	Region, Account string
	Settings        TranscriptionSettings
	Tags            pulumi.StringMap
	Code            pulumi.ArchiveInput
	Architecture    string
}
type Transcription struct {
	pulumi.ResourceState
	StateMachineARN, DeadLetterURL pulumi.StringOutput
}

func createTranscription(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Transcription, error) {
	if err := args.Settings.Validate(); err != nil {
		return nil, err
	}
	if args.Code == nil {
		return nil, fmt.Errorf("transcription Lambda archive is required")
	}
	t := &Transcription{}
	if err := ctx.RegisterComponentResource("lyrics:transcription:Transcription", name, t, opts...); err != nil {
		return nil, err
	}
	b := builder{ctx: ctx, name: name, args: args, parent: pulumi.Parent(t), component: t}
	machine, err := b.create()
	if err != nil {
		return nil, err
	}
	t.StateMachineARN = machine.Arn
	if err = ctx.RegisterResourceOutputs(t, pulumi.Map{"stateMachineArn": t.StateMachineARN, "deadLetterQueueUrl": t.DeadLetterURL}); err != nil {
		return nil, err
	}
	return t, nil
}

type builder struct {
	ctx       *pulumi.Context
	name      string
	args      Args
	parent    pulumi.ResourceOption
	component *Transcription
}

func (b builder) role(suffix, service string, source pulumi.StringInput) (*roles.Role, error) {
	return roles.NewRole(b.ctx, b.name+"-"+suffix, roles.Args{Trust: trust(b.ctx, service, b.args.Account, source), Tags: b.args.Tags}, b.parent)
}
func (b builder) policy(suffix string, role *roles.Role, statements ...iam.GetPolicyDocumentStatementArgs) (*iam.RolePolicy, error) {
	return roles.AttachPolicy(b.ctx, b.name+"-"+suffix, role.Name, document(b.ctx, statements...), b.parent)
}
func (b builder) create() (*sfn.StateMachine, error) {
	a, s := b.args, b.args.Settings
	input := pulumi.Sprintf("%s/%s/*", a.Base.BucketARN, a.Base.Objects.Output)
	output := pulumi.Sprintf("%s/%s/*/transcription/*", a.Base.BucketARN, a.Base.Objects.Output)
	jobs := pulumi.Sprintf("arn:aws:transcribe:%s:%s:transcription-job/lyrics-*", a.Region, a.Account)
	dataRole, err := b.role("transcribe-role", "transcribe.amazonaws.com", pulumi.Sprintf("arn:aws:transcribe:%s:%s:*", a.Region, a.Account))
	if err != nil {
		return nil, err
	}
	dataPolicy, err := b.policy("transcribe-policy", dataRole,
		allow([]string{"s3:ListBucket"}, a.Base.BucketARN),
		allow([]string{"s3:GetObject"}, input))
	if err != nil {
		return nil, err
	}
	prepare, err := b.function("prepare", input, output)
	if err != nil {
		return nil, err
	}
	finalize, err := b.function("finalize", input, output)
	if err != nil {
		return nil, err
	}
	workflowRole, err := b.role("workflow-role", "states.amazonaws.com", pulumi.Sprintf("arn:aws:states:%s:%s:stateMachine:*", a.Region, a.Account))
	if err != nil {
		return nil, err
	}
	passRole := allow([]string{"iam:PassRole"}, dataRole.ARN)
	passRole.Conditions = iam.GetPolicyDocumentStatementConditionArray{iam.GetPolicyDocumentStatementConditionArgs{Test: pulumi.String("StringEquals"), Variable: pulumi.String("iam:PassedToService"), Values: pulumi.StringArray{pulumi.String("transcribe.amazonaws.com")}}}
	// Omit output locations so Transcribe uses service-managed storage and never
	// writes an access-check file into the media bucket. Enforce this in IAM too.
	startJob := allow([]string{"transcribe:StartTranscriptionJob"}, pulumi.String("*"))
	startJob.Conditions = iam.GetPolicyDocumentStatementConditionArray{
		iam.GetPolicyDocumentStatementConditionArgs{Test: pulumi.String("Null"), Variable: pulumi.String("transcribe:OutputBucketName"), Values: pulumi.StringArray{pulumi.String("true")}},
		iam.GetPolicyDocumentStatementConditionArgs{Test: pulumi.String("Null"), Variable: pulumi.String("transcribe:OutputKey"), Values: pulumi.StringArray{pulumi.String("true")}},
	}
	workflowPolicy, err := b.policy("workflow-policy", workflowRole, allow([]string{"lambda:InvokeFunction"}, prepare.Arn, finalize.Arn), startJob, allow([]string{"transcribe:GetTranscriptionJob"}, jobs), passRole,
		// StartTranscriptionJob also authorizes access to the requested S3 locations.
		allow([]string{"s3:ListBucket"}, a.Base.BucketARN),
		allow([]string{"s3:GetObject"}, input))
	if err != nil {
		return nil, err
	}
	definition := pulumi.JSONMarshal(pulumix.Apply3(prepare.Arn, finalize.Arn, dataRole.ARN, func(p, f, r string) machineDefinition { return Definition(s, p, f, r) }))
	machine, err := sfn.NewStateMachine(b.ctx, b.name+"-workflow", &sfn.StateMachineArgs{RoleArn: workflowRole.ARN, Type: pulumi.String("STANDARD"), Definition: definition, Tags: a.Tags}, b.parent, pulumi.DependsOn([]pulumi.Resource{workflowPolicy, dataPolicy}))
	if err != nil {
		return nil, err
	}
	dlq, err := b.events(machine)
	if err != nil {
		return nil, err
	}
	// Return outputs through the component in New, without depending on provider side effects.
	b.component.DeadLetterURL = dlq.Url
	if err = b.alarms(machine, prepare, finalize, dlq); err != nil {
		return nil, err
	}
	return machine, nil
}
func (b builder) function(action string, input, output pulumi.StringInput) (*lambda.Function, error) {
	a, s := b.args, b.args.Settings
	logs, err := cloudwatch.NewLogGroup(b.ctx, b.name+"-"+action+"-logs", &cloudwatch.LogGroupArgs{NamePrefix: pulumi.String("/aws/lambda/" + b.name + "-" + action + "-"), RetentionInDays: pulumi.Int(s.LogRetentionDays), Tags: a.Tags}, b.parent)
	if err != nil {
		return nil, err
	}
	role, err := b.role(action+"-role", "lambda.amazonaws.com", nil)
	if err != nil {
		return nil, err
	}
	list := allow([]string{"s3:ListBucket"}, a.Base.BucketARN)
	list.Conditions = iam.GetPolicyDocumentStatementConditionArray{iam.GetPolicyDocumentStatementConditionArgs{Test: pulumi.String("StringLike"), Variable: pulumi.String("s3:prefix"), Values: pulumi.StringArray{pulumi.Sprintf("%s/*/transcription/*", a.Base.Objects.Output)}}}
	statements := []iam.GetPolicyDocumentStatementArgs{list, allow([]string{"s3:GetObject"}, input, output), allow([]string{"s3:PutObject"}, output), allow([]string{"logs:CreateLogStream", "logs:PutLogEvents"}, pulumi.Sprintf("%s:*", logs.Arn))}
	if action == "finalize" {
		jobs := pulumi.Sprintf("arn:aws:transcribe:%s:%s:transcription-job/lyrics-*", a.Region, a.Account)
		statements = append(statements, allow([]string{"transcribe:GetTranscriptionJob"}, jobs))
	}
	permissions, err := b.policy(action+"-policy", role, statements...)
	if err != nil {
		return nil, err
	}

	return functions.New(b.ctx, b.name+"-"+action, functions.Args{Artifact: a.Code, Runtime: "provided.al2023", Handler: "bootstrap", Architecture: a.Architecture, RoleARN: role.ARN, LogGroup: logs.Name, TimeoutSeconds: s.LambdaTimeoutSeconds, MemoryMiB: s.LambdaMemoryMiB, Environment: pulumi.StringMap{"MEDIA_BUCKET": a.Base.BucketName, "OUTPUT_PREFIX": a.Base.Objects.Output, "ACCOUNT_ID": pulumi.String(a.Account), "LANGUAGE": pulumi.String(s.Language), "PIPELINE_VERSION": pulumi.String(s.PipelineVersion), "TRANSCRIPTION_ACTION": pulumi.String(action)}, Tags: a.Tags}, b.parent, pulumi.DependsOn([]pulumi.Resource{permissions}))
}
