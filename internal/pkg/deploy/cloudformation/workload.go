// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package cloudformation

import (
	"errors"
	"fmt"

	"github.com/aws/copilot-cli/internal/pkg/aws/cloudformation"
	"github.com/aws/copilot-cli/internal/pkg/deploy"
)

// DeployService deploys a service stack and waits until the deployment is done.
// If the service stack doesn't exist, then it creates the stack.
// If the service stack already exists, it updates the stack.
func (cf CloudFormation) DeployService(conf StackConfiguration, opts ...cloudformation.StackOption) error {
	stack, err := toStack(conf)
	if err != nil {
		return err
	}
	for _, opt := range opts {
		opt(stack)
	}

	err = cf.cfnClient.CreateAndWait(stack)
	if err == nil { // Created a new stack, stop execution.
		return nil
	}
	// The stack already exists, we need to update it instead.
	var errAlreadyExists *cloudformation.ErrStackAlreadyExists
	if !errors.As(err, &errAlreadyExists) {
		return cf.handleStackError(conf, err)
	}
	err = cf.cfnClient.UpdateAndWait(stack)
	return cf.handleStackError(conf, err)
}

func (cf CloudFormation) handleStackError(conf StackConfiguration, err error) error {
	if err == nil {
		return nil
	}
	errors, describeErr := cf.ErrorEvents(conf)
	if describeErr != nil {
		return fmt.Errorf("%w: describe stack: %v", err, describeErr)
	}
	if len(errors) == 0 {
		return err
	}
	return fmt.Errorf("%w: %s", err, errors[0].StatusReason)
}

// DeleteWorkload removes the CloudFormation stack of a deployed workload.
func (cf CloudFormation) DeleteWorkload(in deploy.DeleteWorkloadInput) error {
	return cf.cfnClient.DeleteAndWait(fmt.Sprintf("%s-%s-%s", in.AppName, in.EnvName, in.Name))
}
