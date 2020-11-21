// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/copilot-cli/internal/pkg/template"
	"github.com/imdario/mergo"
	"gopkg.in/yaml.v3"
)

const (
	lbWebSvcManifestPath = "workloads/services/lb-web/manifest.yml"
)

// Default values for HTTPHealthCheck for a load balanced web service.
const (
	// LogRetentionInDays is the default log retention time in days.
	LogRetentionInDays     = 30
	defaultHealthCheckPath = "/"
)

var (
	errUnmarshalHealthCheckArgs = errors.New("can't unmarshal healthcheck field into string or compose-style map")
)

// LoadBalancedWebService holds the configuration to build a container image with an exposed port that receives
// requests through a load balancer with AWS Fargate as the compute engine.
type LoadBalancedWebService struct {
	Workload                     `yaml:",inline"`
	LoadBalancedWebServiceConfig `yaml:",inline"`
	// Use *LoadBalancedWebServiceConfig because of https://github.com/imdario/mergo/issues/146
	Environments map[string]*LoadBalancedWebServiceConfig `yaml:",flow"` // Fields to override per environment.

	parser template.Parser
}

// LoadBalancedWebServiceConfig holds the configuration for a load balanced web service.
type LoadBalancedWebServiceConfig struct {
	ImageConfig ServiceImageWithPort `yaml:"image,flow"`
	RoutingRule `yaml:"http,flow"`
	TaskConfig  `yaml:",inline"`
	*Logging    `yaml:"logging,flow"`
	Sidecar     `yaml:",inline"`
}

// LogConfigOpts converts the service's Firelens configuration into a format parsable by the templates pkg.
func (lc *LoadBalancedWebServiceConfig) LogConfigOpts() *template.LogConfigOpts {
	if lc.Logging == nil {
		return nil
	}
	return lc.logConfigOpts()
}

// HTTPHealthCheckArgs holds the configuration to determine if the load balanced web service is healthy.
// These options are specifiable under the "healthcheck" field.
// See https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-elasticloadbalancingv2-targetgroup.html.
type HTTPHealthCheckArgs struct {
	Path               *string        `yaml:"path"`
	HealthyThreshold   *int64         `yaml:"healthy_threshold"`
	UnhealthyThreshold *int64         `yaml:"unhealthy_threshold"`
	Timeout            *time.Duration `yaml:"timeout"`
	Interval           *time.Duration `yaml:"interval"`
}

// HealthCheckArgsOrString is a custom type which supports unmarshaling yaml which
// can either be of type string or type HealthCheckArgs.
type HealthCheckArgsOrString struct {
	HealthCheckPath *string
	HealthCheckArgs HTTPHealthCheckArgs
}

// HTTPHealthCheckOpts converts the ALB health check configuration into a format parsable by the templates pkg.
func (hc HealthCheckArgsOrString) HTTPHealthCheckOpts() template.HTTPHealthCheckOpts {
	opts := template.HTTPHealthCheckOpts{
		HealthCheckPath:    defaultHealthCheckPath,
		HealthyThreshold:   hc.HealthCheckArgs.HealthyThreshold,
		UnhealthyThreshold: hc.HealthCheckArgs.UnhealthyThreshold,
	}
	if hc.HealthCheckArgs.Path != nil {
		opts.HealthCheckPath = *hc.HealthCheckArgs.Path
	} else if hc.HealthCheckPath != nil {
		opts.HealthCheckPath = *hc.HealthCheckPath
	}
	if hc.HealthCheckArgs.Interval != nil {
		opts.Interval = aws.Int64(int64(hc.HealthCheckArgs.Interval.Seconds()))
	}
	if hc.HealthCheckArgs.Timeout != nil {
		opts.Timeout = aws.Int64(int64(hc.HealthCheckArgs.Timeout.Seconds()))
	}
	return opts
}

// UnmarshalYAML overrides the default YAML unmarshaling logic for the HealthCheckArgsOrString
// struct, allowing it to perform more complex unmarshaling behavior.
// This method implements the yaml.Unmarshaler (v2) interface.
func (hc *HealthCheckArgsOrString) UnmarshalYAML(unmarshal func(interface{}) error) error {
	if err := unmarshal(&hc.HealthCheckArgs); err != nil {
		switch err.(type) {
		case *yaml.TypeError:
			break
		default:
			return err
		}
	}

	if !hc.HealthCheckArgs.isEmpty() {
		// Unmarshaled successfully to hc.HealthCheckArgs, reset hc.HealthCheckPath, and return.
		hc.HealthCheckPath = nil
		return nil
	}

	if err := unmarshal(&hc.HealthCheckPath); err != nil {
		return errUnmarshalHealthCheckArgs
	}
	return nil
}

// RoutingRule holds the path to route requests to the service.
type RoutingRule struct {
	Path        *string                 `yaml:"path"`
	HealthCheck HealthCheckArgsOrString `yaml:"healthcheck"`
	Stickiness  *bool                   `yaml:"stickiness"`
	// TargetContainer is the container load balancer routes traffic to.
	TargetContainer          *string  `yaml:"target_container"`
	TargetContainerCamelCase *string  `yaml:"targetContainer"` // "targetContainerCamelCase" for backwards compatibility
	AllowedSourceIps         []string `yaml:"allowed_source_ips"`
}

// LoadBalancedWebServiceProps contains properties for creating a new load balanced fargate service manifest.
type LoadBalancedWebServiceProps struct {
	*WorkloadProps
	Path string
	Port uint16
}

// NewLoadBalancedWebService creates a new public load balanced web service, receives all the requests from the load balancer,
// has a single task with minimal CPU and memory thresholds, and sets the default health check path to "/".
func NewLoadBalancedWebService(props *LoadBalancedWebServiceProps) *LoadBalancedWebService {
	svc := newDefaultLoadBalancedWebService()
	// Apply overrides.
	svc.Name = aws.String(props.Name)
	svc.LoadBalancedWebServiceConfig.ImageConfig.Image.Location = stringP(props.Image)
	svc.LoadBalancedWebServiceConfig.ImageConfig.Build.BuildArgs.Dockerfile = stringP(props.Dockerfile)
	svc.LoadBalancedWebServiceConfig.ImageConfig.Port = aws.Uint16(props.Port)
	svc.RoutingRule.Path = aws.String(props.Path)
	svc.parser = template.New()
	return svc
}

// newDefaultLoadBalancedWebService returns an empty LoadBalancedWebService with only the default values set.
func newDefaultLoadBalancedWebService() *LoadBalancedWebService {
	return &LoadBalancedWebService{
		Workload: Workload{
			Type: aws.String(LoadBalancedWebServiceType),
		},
		LoadBalancedWebServiceConfig: LoadBalancedWebServiceConfig{
			ImageConfig: ServiceImageWithPort{},
			RoutingRule: RoutingRule{
				HealthCheck: HealthCheckArgsOrString{
					HealthCheckPath: aws.String(defaultHealthCheckPath),
				},
			},
			TaskConfig: TaskConfig{
				CPU:    aws.Int(256),
				Memory: aws.Int(512),
				Count: Count{
					Value: aws.Int(1),
				},
			},
		},
	}
}

func (h *HTTPHealthCheckArgs) isEmpty() bool {
	return h.Path == nil && h.HealthyThreshold == nil && h.UnhealthyThreshold == nil && h.Interval == nil && h.Timeout == nil
}

// MarshalBinary serializes the manifest object into a binary YAML document.
// Implements the encoding.BinaryMarshaler interface.
func (s *LoadBalancedWebService) MarshalBinary() ([]byte, error) {
	content, err := s.parser.Parse(lbWebSvcManifestPath, *s, template.WithFuncs(map[string]interface{}{
		"dirName": tplDirName,
	}))
	if err != nil {
		return nil, err
	}
	return content.Bytes(), nil
}

func tplDirName(s string) string {
	return filepath.Dir(s)
}

// BuildRequired returns if the service requires building from the local Dockerfile.
func (s *LoadBalancedWebService) BuildRequired() (bool, error) {
	return requiresBuild(s.ImageConfig.Image)
}

// BuildArgs returns a docker.BuildArguments object given a ws root directory.
func (s *LoadBalancedWebService) BuildArgs(wsRoot string) *DockerBuildArgs {
	return s.ImageConfig.BuildConfig(wsRoot)
}

// ApplyEnv returns the service manifest with environment overrides.
// If the environment passed in does not have any overrides then it returns itself.
func (s LoadBalancedWebService) ApplyEnv(envName string) (*LoadBalancedWebService, error) {
	overrideConfig, ok := s.Environments[envName]
	if !ok {
		return &s, nil
	}
	// Apply overrides to the original service s.
	err := mergo.Merge(&s, LoadBalancedWebService{
		LoadBalancedWebServiceConfig: *overrideConfig,
	}, mergo.WithOverride, mergo.WithOverwriteWithEmptyValue)
	if err != nil {
		return nil, err
	}
	s.Environments = nil
	return &s, nil
}
