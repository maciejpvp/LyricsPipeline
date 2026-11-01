package main

// Concrete ASL wire types keep SDK parameters and JSON paths reviewable.
type machineDefinition struct {
	StartAt        string           `json:"StartAt"`
	TimeoutSeconds int              `json:"TimeoutSeconds"`
	States         map[string]state `json:"States"`
}
type state struct {
	Type       string      `json:"Type"`
	Resource   string      `json:"Resource,omitempty"`
	Parameters *parameters `json:"Parameters,omitempty"`
	OutputPath string      `json:"OutputPath,omitempty"`
	ResultPath string      `json:"ResultPath,omitempty"`
	Next       string      `json:"Next,omitempty"`
	End        bool        `json:"End,omitempty"`
	Seconds    int         `json:"Seconds,omitempty"`
	Choices    []choice    `json:"Choices,omitempty"`
	Default    string      `json:"Default,omitempty"`
	Retry      []retry     `json:"Retry,omitempty"`
	Catch      []catcher   `json:"Catch,omitempty"`
	Error      string      `json:"Error,omitempty"`
	CausePath  string      `json:"CausePath,omitempty"`
}
type parameters struct {
	FunctionName         string                `json:"FunctionName,omitempty"`
	PayloadPath          string                `json:"Payload.$,omitempty"`
	JobNamePath          string                `json:"TranscriptionJobName.$,omitempty"`
	LanguageCode         string                `json:"LanguageCode,omitempty"`
	MediaFormat          string                `json:"MediaFormat,omitempty"`
	Media                *media                `json:"Media,omitempty"`
	JobExecutionSettings *jobExecutionSettings `json:"JobExecutionSettings,omitempty"`
}
type media struct {
	URIPath string `json:"MediaFileUri.$"`
}
type jobExecutionSettings struct {
	AllowDeferredExecution bool   `json:"AllowDeferredExecution"`
	DataAccessRoleARN      string `json:"DataAccessRoleArn"`
}
type choice struct {
	Variable      string `json:"Variable"`
	BooleanEquals *bool  `json:"BooleanEquals,omitempty"`
	StringEquals  string `json:"StringEquals,omitempty"`
	Next          string `json:"Next"`
}
type retry struct {
	ErrorEquals     []string `json:"ErrorEquals"`
	IntervalSeconds int      `json:"IntervalSeconds"`
	MaxAttempts     int      `json:"MaxAttempts"`
	BackoffRate     float64  `json:"BackoffRate"`
}
type catcher struct {
	ErrorEquals []string `json:"ErrorEquals"`
	ResultPath  string   `json:"ResultPath"`
	Next        string   `json:"Next"`
}

func Definition(s TranscriptionSettings, prepare, finalize, dataRole string) machineDefinition {
	apiRetry := []retry{{ErrorEquals: []string{"Transcribe.InternalFailureException", "Transcribe.LimitExceededException", "Transcribe.ThrottlingException"}, IntervalSeconds: s.RetryIntervalSeconds, MaxAttempts: s.RetryAttempts, BackoffRate: 2}}
	lambdaRetry := []retry{{ErrorEquals: []string{"Lambda.ServiceException", "Lambda.AWSLambdaException", "Lambda.SdkClientException", "Lambda.TooManyRequestsException", "TransientError"}, IntervalSeconds: s.RetryIntervalSeconds, MaxAttempts: s.RetryAttempts, BackoffRate: 2}}
	yes := true
	return machineDefinition{StartAt: "Prepare", TimeoutSeconds: s.TimeoutSeconds, States: map[string]state{
		"Prepare":             {Type: "Task", Resource: "arn:aws:states:::lambda:invoke", Parameters: &parameters{FunctionName: prepare, PayloadPath: "$"}, OutputPath: "$.Payload", Retry: lambdaRetry, Next: "Cached"},
		"Cached":              {Type: "Choice", Choices: []choice{{Variable: "$.cached", BooleanEquals: &yes, Next: "AlreadyComplete"}}, Default: "StartTranscription"},
		"AlreadyComplete":     {Type: "Succeed"},
		"StartTranscription":  {Type: "Task", Resource: "arn:aws:states:::aws-sdk:transcribe:startTranscriptionJob", Parameters: &parameters{JobNamePath: "$.job_name", LanguageCode: s.Language, MediaFormat: "wav", Media: &media{URIPath: "$.media_uri"}, JobExecutionSettings: &jobExecutionSettings{true, dataRole}}, ResultPath: "$.transcribe", Retry: apiRetry, Catch: []catcher{{[]string{"Transcribe.ConflictException"}, "$.start_error", "GetTranscription"}}, Next: "Wait"},
		"Wait":                {Type: "Wait", Seconds: s.PollSeconds, Next: "GetTranscription"},
		"GetTranscription":    {Type: "Task", Resource: "arn:aws:states:::aws-sdk:transcribe:getTranscriptionJob", Parameters: &parameters{JobNamePath: "$.job_name"}, ResultPath: "$.transcribe", Retry: apiRetry, Next: "Status"},
		"Status":              {Type: "Choice", Choices: []choice{{Variable: "$.transcribe.TranscriptionJob.TranscriptionJobStatus", StringEquals: "COMPLETED", Next: "Finalize"}, {Variable: "$.transcribe.TranscriptionJob.TranscriptionJobStatus", StringEquals: "FAILED", Next: "TranscriptionFailed"}}, Default: "Wait"},
		"TranscriptionFailed": {Type: "Fail", Error: "TranscriptionFailed", CausePath: "$.transcribe.TranscriptionJob.FailureReason"},
		"Finalize":            {Type: "Task", Resource: "arn:aws:states:::lambda:invoke", Parameters: &parameters{FunctionName: finalize, PayloadPath: "$"}, OutputPath: "$.Payload", Retry: lambdaRetry, End: true},
	}}
}
