// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package stack

import (
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/copilot-cli/internal/pkg/addon"
	"github.com/aws/copilot-cli/internal/pkg/manifest"
	"github.com/aws/copilot-cli/internal/pkg/template"
)

// Template rendering configuration.
const (
	lbWebSvcRulePriorityGeneratorPath = "custom-resources/alb-rule-priority-generator.js"
	desiredCountGeneratorPath         = "custom-resources/desired-count-delegation.js"
	envControllerPath                 = "custom-resources/env-controller.js"
)

// Parameter logical IDs for a load balanced web service.
const (
	LBWebServiceHTTPSParamKey           = "HTTPSEnabled"
	LBWebServiceContainerPortParamKey   = "ContainerPort"
	LBWebServiceRulePathParamKey        = "RulePath"
	LBWebServiceTargetContainerParamKey = "TargetContainer"
	LBWebServiceTargetPortParamKey      = "TargetPort"
	LBWebServiceStickinessParamKey      = "Stickiness"
)

type loadBalancedWebSvcReadParser interface {
	template.ReadParser
	ParseLoadBalancedWebService(template.WorkloadOpts) (*template.Content, error)
}

// LoadBalancedWebService represents the configuration needed to create a CloudFormation stack from a load balanced web service manifest.
type LoadBalancedWebService struct {
	*wkld
	manifest     *manifest.LoadBalancedWebService
	httpsEnabled bool

	parser loadBalancedWebSvcReadParser
}

// NewLoadBalancedWebService creates a new LoadBalancedWebService stack from a manifest file.
func NewLoadBalancedWebService(mft *manifest.LoadBalancedWebService, env, app string, rc RuntimeConfig) (*LoadBalancedWebService, error) {
	parser := template.New()
	addons, err := addon.New(aws.StringValue(mft.Name))
	if err != nil {
		return nil, fmt.Errorf("new addons: %w", err)
	}
	envManifest, err := mft.ApplyEnv(env) // Apply environment overrides to the manifest values.
	if err != nil {
		return nil, fmt.Errorf("apply environment %s override: %s", env, err)
	}
	return &LoadBalancedWebService{
		wkld: &wkld{
			name:   aws.StringValue(mft.Name),
			env:    env,
			app:    app,
			tc:     envManifest.TaskConfig,
			rc:     rc,
			image:  envManifest.ImageConfig,
			parser: parser,
			addons: addons,
		},
		manifest:     envManifest,
		httpsEnabled: false,

		parser: parser,
	}, nil
}

// NewHTTPSLoadBalancedWebService  creates a new LoadBalancedWebService stack from its manifest that needs to be deployed to
// a environment within an application. It creates an HTTPS listener and assumes that the environment
// it's being deployed into has an HTTPS configured listener.
func NewHTTPSLoadBalancedWebService(mft *manifest.LoadBalancedWebService, env, app string, rc RuntimeConfig) (*LoadBalancedWebService, error) {
	webSvc, err := NewLoadBalancedWebService(mft, env, app, rc)
	if err != nil {
		return nil, err
	}
	webSvc.httpsEnabled = true
	return webSvc, nil
}

// Template returns the CloudFormation template for the service parametrized for the environment.
func (s *LoadBalancedWebService) Template() (string, error) {
	rulePriorityLambda, err := s.parser.Read(lbWebSvcRulePriorityGeneratorPath)
	if err != nil {
		return "", fmt.Errorf("read rule priority lambda: %w", err)
	}
	desiredCountLambda, err := s.parser.Read(desiredCountGeneratorPath)
	if err != nil {
		return "", fmt.Errorf("read desired count lambda: %w", err)
	}
	envControllerLambda, err := s.parser.Read(envControllerPath)
	if err != nil {
		return "", fmt.Errorf("read env controller lambda: %w", err)
	}
	outputs, err := s.addonsOutputs()
	if err != nil {
		return "", err
	}
	sidecars, err := s.manifest.Sidecar.Options()
	if err != nil {
		return "", fmt.Errorf("convert the sidecar configuration for service %s: %w", s.name, err)
	}
	autoscaling, err := s.manifest.Count.Autoscaling.Options()
	if err != nil {
		return "", fmt.Errorf("convert the Auto Scaling configuration for service %s: %w", s.name, err)
	}
	content, err := s.parser.ParseLoadBalancedWebService(template.WorkloadOpts{
		Variables:           s.manifest.Variables,
		Secrets:             s.manifest.Secrets,
		NestedStack:         outputs,
		Sidecars:            sidecars,
		LogConfig:           s.manifest.LogConfigOpts(),
		Autoscaling:         autoscaling,
		HTTPHealthCheck:     s.manifest.HealthCheck.HTTPHealthCheckOpts(),
		AllowedSourceIps:    s.manifest.AllowedSourceIps,
		RulePriorityLambda:  rulePriorityLambda.String(),
		DesiredCountLambda:  desiredCountLambda.String(),
		EnvControllerLambda: envControllerLambda.String(),
	})
	if err != nil {
		return "", err
	}
	return content.String(), nil
}

func (s *LoadBalancedWebService) loadBalancerTarget() (targetContainer *string, targetPort *string, err error) {
	containerName := s.name
	containerPort := strconv.FormatUint(uint64(aws.Uint16Value(s.manifest.ImageConfig.Port)), 10)
	// Route load balancer traffic to main container by default.
	targetContainer = aws.String(containerName)
	targetPort = aws.String(containerPort)
	if s.manifest.TargetContainer == nil && s.manifest.TargetContainerCamelCase != nil {
		s.manifest.TargetContainer = s.manifest.TargetContainerCamelCase
	}
	mftTargetContainer := s.manifest.TargetContainer
	if mftTargetContainer != nil {
		sidecar, ok := s.manifest.Sidecars[*mftTargetContainer]
		if ok {
			targetContainer = mftTargetContainer
			targetPort = sidecar.Port
		} else {
			return nil, nil, fmt.Errorf("target container %s doesn't exist", *s.manifest.TargetContainer)
		}
	}
	return
}

// Parameters returns the list of CloudFormation parameters used by the template.
func (s *LoadBalancedWebService) Parameters() ([]*cloudformation.Parameter, error) {
	wkldParams, err := s.wkld.Parameters()
	if err != nil {
		return nil, err
	}
	targetContainer, targetPort, err := s.loadBalancerTarget()
	if err != nil {
		return nil, err
	}
	return append(wkldParams, []*cloudformation.Parameter{
		{
			ParameterKey:   aws.String(LBWebServiceContainerPortParamKey),
			ParameterValue: aws.String(strconv.FormatUint(uint64(aws.Uint16Value(s.manifest.ImageConfig.Port)), 10)),
		},
		{
			ParameterKey:   aws.String(LBWebServiceRulePathParamKey),
			ParameterValue: s.manifest.Path,
		},
		{
			ParameterKey:   aws.String(LBWebServiceHTTPSParamKey),
			ParameterValue: aws.String(strconv.FormatBool(s.httpsEnabled)),
		},
		{
			ParameterKey:   aws.String(LBWebServiceTargetContainerParamKey),
			ParameterValue: targetContainer,
		},
		{
			ParameterKey:   aws.String(LBWebServiceTargetPortParamKey),
			ParameterValue: targetPort,
		},
		{
			ParameterKey:   aws.String(LBWebServiceStickinessParamKey),
			ParameterValue: aws.String(strconv.FormatBool(aws.BoolValue(s.manifest.Stickiness))),
		},
	}...), nil
}

// SerializedParameters returns the CloudFormation stack's parameters serialized
// to a YAML document annotated with comments for readability to users.
func (s *LoadBalancedWebService) SerializedParameters() (string, error) {
	return s.wkld.templateConfiguration(s)
}
