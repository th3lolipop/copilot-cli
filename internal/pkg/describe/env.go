// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package describe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/aws/copilot-cli/internal/pkg/aws/sessions"
	"github.com/aws/copilot-cli/internal/pkg/config"
	"github.com/aws/copilot-cli/internal/pkg/deploy"
	"github.com/aws/copilot-cli/internal/pkg/deploy/cloudformation/stack"
	"github.com/aws/copilot-cli/internal/pkg/term/color"
	"gopkg.in/yaml.v3"
)

// EnvDescription contains the information about an environment.
type EnvDescription struct {
	Environment *config.Environment `json:"environment"`
	Services    []*config.Workload  `json:"services"`
	Tags        map[string]string   `json:"tags,omitempty"`
	Resources   []*CfnResource      `json:"resources,omitempty"`
}

// EnvDescriber retrieves information about an environment.
type EnvDescriber struct {
	app             string
	env             *config.Environment
	enableResources bool

	configStore    ConfigStoreSvc
	deployStore    DeployedEnvServicesLister
	stackDescriber stackAndResourcesDescriber
}

// NewEnvDescriberConfig contains fields that initiates EnvDescriber struct.
type NewEnvDescriberConfig struct {
	App             string
	Env             string
	EnableResources bool
	ConfigStore     ConfigStoreSvc
	DeployStore     DeployedEnvServicesLister
}

// NewEnvDescriber instantiates an environment describer.
func NewEnvDescriber(opt NewEnvDescriberConfig) (*EnvDescriber, error) {
	env, err := opt.ConfigStore.GetEnvironment(opt.App, opt.Env)
	if err != nil {
		return nil, fmt.Errorf("get environment: %w", err)
	}
	sess, err := sessions.NewProvider().FromRole(env.ManagerRoleARN, env.Region)
	if err != nil {
		return nil, fmt.Errorf("assume role for environment %s: %w", env.ManagerRoleARN, err)
	}
	d := newStackDescriber(sess)
	return &EnvDescriber{
		app:             opt.App,
		env:             env,
		enableResources: opt.EnableResources,

		configStore:    opt.ConfigStore,
		deployStore:    opt.DeployStore,
		stackDescriber: d,
	}, nil
}

// Describe returns info about an application's environment.
func (d *EnvDescriber) Describe() (*EnvDescription, error) {
	svcs, err := d.filterDeployedSvcs()
	if err != nil {
		return nil, err
	}

	tags, err := d.stackTags()
	if err != nil {
		return nil, fmt.Errorf("retrieve environment tags: %w", err)
	}

	var stackResources []*CfnResource
	if d.enableResources {
		stackResources, err = d.resources()
		if err != nil {
			return nil, fmt.Errorf("retrieve environment resources: %w", err)
		}
	}

	return &EnvDescription{
		Environment: d.env,
		Services:    svcs,
		Tags:        tags,
		Resources:   stackResources,
	}, nil
}

// Version returns the CloudFormation template version associated with
// the environment by reading the Metadata.Version field from the template.
//
// If the Version field does not exist, then it's a legacy template and it returns an deploy.LegacyEnvTemplateVersion and nil error.
func (d *EnvDescriber) Version() (string, error) {
	raw, err := d.stackDescriber.Metadata(stack.NameForEnv(d.app, d.env.Name))
	if err != nil {
		return "", err
	}

	metadata := struct {
		Version string `yaml:"Version"`
	}{}
	if err := yaml.Unmarshal([]byte(raw), &metadata); err != nil {
		return "", fmt.Errorf("unmarshal Metadata property to read Version: %w", err)
	}
	if metadata.Version == "" {
		return deploy.LegacyEnvTemplateVersion, nil
	}
	return metadata.Version, nil
}

// EnvironmentVPC holds the ID of the environment's VPC configuration.
type EnvironmentVPC struct {
	ID               string
	PublicSubnetIDs  []string
	PrivateSubnetIDs []string
}

func (d *EnvDescriber) stackTags() (map[string]string, error) {
	tags := make(map[string]string)
	envStack, err := d.stackDescriber.Stack(stack.NameForEnv(d.app, d.env.Name))
	if err != nil {
		return nil, err
	}
	for _, tag := range envStack.Tags {
		tags[*tag.Key] = *tag.Value
	}
	return tags, nil
}

func (d *EnvDescriber) filterDeployedSvcs() ([]*config.Workload, error) {
	allSvcs, err := d.configStore.ListServices(d.app)
	if err != nil {
		return nil, fmt.Errorf("list services for app %s: %w", d.app, err)
	}
	svcs := make(map[string]*config.Workload)
	for _, svc := range allSvcs {
		svcs[svc.Name] = svc
	}
	deployedSvcNames, err := d.deployStore.ListDeployedServices(d.app, d.env.Name)
	if err != nil {
		return nil, fmt.Errorf("list deployed services in env %s: %w", d.env.Name, err)
	}
	var deployedSvcs []*config.Workload
	for _, deployedSvcName := range deployedSvcNames {
		deployedSvcs = append(deployedSvcs, svcs[deployedSvcName])
	}
	return deployedSvcs, nil
}

func (d *EnvDescriber) resources() ([]*CfnResource, error) {
	envStack, err := d.stackDescriber.StackResources(stack.NameForEnv(d.app, d.env.Name))
	if err != nil {
		return nil, err
	}
	outputs := flattenResources(envStack)
	return outputs, nil
}

// JSONString returns the stringified EnvDescription struct with json format.
func (e *EnvDescription) JSONString() (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("marshal environment description: %w", err)
	}
	return fmt.Sprintf("%s\n", b), nil
}

// HumanString returns the stringified EnvDescription struct with human readable format.
func (e *EnvDescription) HumanString() string {
	var b bytes.Buffer
	writer := tabwriter.NewWriter(&b, minCellWidth, tabWidth, cellPaddingWidth, paddingChar, noAdditionalFormatting)
	fmt.Fprint(writer, color.Bold.Sprint("About\n\n"))
	writer.Flush()
	fmt.Fprintf(writer, "  %s\t%s\n", "Name", e.Environment.Name)
	fmt.Fprintf(writer, "  %s\t%t\n", "Production", e.Environment.Prod)
	fmt.Fprintf(writer, "  %s\t%s\n", "Region", e.Environment.Region)
	fmt.Fprintf(writer, "  %s\t%s\n", "Account ID", e.Environment.AccountID)
	fmt.Fprint(writer, color.Bold.Sprint("\nServices\n\n"))
	writer.Flush()
	fmt.Fprintf(writer, "  %s\t%s\n", "Name", "Type")
	nameLengthMax := len("Name")
	typeLengthMax := len("Type")
	for _, svc := range e.Services {
		nameLengthMax = int(math.Max(float64(nameLengthMax), float64(len(svc.Name))))
		typeLengthMax = int(math.Max(float64(typeLengthMax), float64(len(svc.Type))))
	}
	fmt.Fprintf(writer, "  %s\t%s\n", strings.Repeat("-", nameLengthMax), strings.Repeat("-", typeLengthMax))
	writer.Flush()
	for _, svc := range e.Services {
		fmt.Fprintf(writer, "  %s\t%s\n", svc.Name, svc.Type)
	}
	writer.Flush()
	if len(e.Tags) != 0 {
		fmt.Fprint(writer, color.Bold.Sprint("\nTags\n\n"))
		writer.Flush()
		KeyLengthMax := len("Key")
		ValueLengthMax := len("Value")
		for k, v := range e.Tags {
			KeyLengthMax = int(math.Max(float64(KeyLengthMax), float64(len(k))))
			ValueLengthMax = int(math.Max(float64(ValueLengthMax), float64(len(v))))
		}
		fmt.Fprintf(writer, "  %s\t%s\n", "Key", "Value")
		fmt.Fprintf(writer, "  %s\t%s\n", strings.Repeat("-", KeyLengthMax), strings.Repeat("-", ValueLengthMax))
		writer.Flush()
		// sort Tags in alpha order by keys
		keys := make([]string, 0, len(e.Tags))
		for k := range e.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(writer, "  %s\t%s\n", key, e.Tags[key])
			writer.Flush()
		}
	}
	writer.Flush()
	if len(e.Resources) != 0 {
		fmt.Fprint(writer, color.Bold.Sprint("\nResources\n\n"))
		writer.Flush()
		for _, resource := range e.Resources {
			fmt.Fprintf(writer, "  %s\t%s\n", resource.Type, resource.PhysicalID)
		}
	}
	writer.Flush()
	return b.String()
}
