package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func allow(actions []string, resources ...pulumi.StringInput) iam.GetPolicyDocumentStatementArgs {
	a := pulumi.StringArray{}
	for _, v := range actions {
		a = append(a, pulumi.String(v))
	}
	r := pulumi.StringArray{}
	for _, v := range resources {
		r = append(r, v)
	}
	return iam.GetPolicyDocumentStatementArgs{Effect: pulumi.String("Allow"), Actions: a, Resources: r}
}
func document(ctx *pulumi.Context, statements ...iam.GetPolicyDocumentStatementArgs) pulumi.StringOutput {
	a := iam.GetPolicyDocumentStatementArray{}
	for _, s := range statements {
		a = append(a, s)
	}
	return iam.GetPolicyDocumentOutput(ctx, iam.GetPolicyDocumentOutputArgs{Version: pulumi.String("2012-10-17"), Statements: a}).Json()
}
func trust(ctx *pulumi.Context, service, account string, source pulumi.StringInput) pulumi.StringOutput {
	s := allow([]string{"sts:AssumeRole"})
	s.Resources = nil
	s.Principals = iam.GetPolicyDocumentStatementPrincipalArray{iam.GetPolicyDocumentStatementPrincipalArgs{Type: pulumi.String("Service"), Identifiers: pulumi.StringArray{pulumi.String(service)}}}
	if source != nil {
		s.Conditions = iam.GetPolicyDocumentStatementConditionArray{
			iam.GetPolicyDocumentStatementConditionArgs{Test: pulumi.String("StringEquals"), Variable: pulumi.String("aws:SourceAccount"), Values: pulumi.StringArray{pulumi.String(account)}},
			iam.GetPolicyDocumentStatementConditionArgs{Test: pulumi.String("ArnLike"), Variable: pulumi.String("aws:SourceArn"), Values: pulumi.StringArray{source}},
		}
	}
	return document(ctx, s)
}
