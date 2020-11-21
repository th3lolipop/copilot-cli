// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/resourcegroupstaggingapi"
	awscfn "github.com/aws/copilot-cli/internal/pkg/aws/cloudformation"
	"github.com/aws/copilot-cli/internal/pkg/aws/iam"
	"github.com/aws/copilot-cli/internal/pkg/aws/sessions"
	"github.com/aws/copilot-cli/internal/pkg/config"
	"github.com/aws/copilot-cli/internal/pkg/deploy"
	"github.com/aws/copilot-cli/internal/pkg/deploy/cloudformation"
	"github.com/aws/copilot-cli/internal/pkg/term/log"
	termprogress "github.com/aws/copilot-cli/internal/pkg/term/progress"
	"github.com/aws/copilot-cli/internal/pkg/term/prompt"
	"github.com/aws/copilot-cli/internal/pkg/term/selector"
	"github.com/spf13/cobra"
)

const (
	envDeleteNamePrompt = "Which environment would you like to delete?"
	fmtDeleteEnvPrompt  = "Are you sure you want to delete environment %s from application %s?"
)

const (
	fmtDeleteEnvStart    = "Deleting environment %s from application %s."
	fmtDeleteEnvFailed   = "Failed to delete environment %s from application %s.\n"
	fmtDeleteEnvComplete = "Deleted environment %s from application %s.\n"
)

var (
	errEnvDeleteCancelled = errors.New("env delete cancelled - no changes made")
)

type resourceGetter interface {
	GetResources(*resourcegroupstaggingapi.GetResourcesInput) (*resourcegroupstaggingapi.GetResourcesOutput, error)
}

type deleteEnvVars struct {
	appName          string
	name             string
	skipConfirmation bool
}

type deleteEnvOpts struct {
	deleteEnvVars

	// Interfaces for dependencies.
	store    environmentStore
	rg       resourceGetter
	deployer environmentDeployer
	iam      roleDeleter
	prog     progress
	prompt   prompter
	sel      configSelector

	// cached data to avoid fetching the same information multiple times.
	envConfig *config.Environment

	// initRuntimeClients is overriden in tests.
	initRuntimeClients func(*deleteEnvOpts) error
}

func newDeleteEnvOpts(vars deleteEnvVars) (*deleteEnvOpts, error) {
	store, err := config.NewStore()
	if err != nil {
		return nil, fmt.Errorf("connect to copilot config store: %w", err)
	}

	prompter := prompt.New()
	return &deleteEnvOpts{
		deleteEnvVars: vars,

		store:  store,
		prog:   termprogress.NewSpinner(),
		sel:    selector.NewConfigSelect(prompter, store),
		prompt: prompter,

		initRuntimeClients: func(o *deleteEnvOpts) error {
			env, err := o.getEnvConfig()
			if err != nil {
				return err
			}
			sess, err := sessions.NewProvider().FromRole(env.ManagerRoleARN, env.Region)
			if err != nil {
				return fmt.Errorf("create session from environment manager role %s in region %s: %w", env.ManagerRoleARN, env.Region, err)
			}
			o.rg = resourcegroupstaggingapi.New(sess)
			o.iam = iam.New(sess)
			o.deployer = cloudformation.New(sess)
			return nil
		},
	}, nil
}

// Validate returns an error if the individual user inputs are invalid.
func (o *deleteEnvOpts) Validate() error {
	if o.name != "" {
		if err := o.validateEnvName(); err != nil {
			return err
		}
	}
	return nil
}

// Ask prompts for fields that are required but not passed in.
func (o *deleteEnvOpts) Ask() error {
	if err := o.askEnvName(); err != nil {
		return err
	}

	if o.skipConfirmation {
		return nil
	}
	deleteConfirmed, err := o.prompt.Confirm(fmt.Sprintf(fmtDeleteEnvPrompt, o.name, o.appName), "")
	if err != nil {
		return fmt.Errorf("confirm to delete environment %s: %w", o.name, err)
	}
	if !deleteConfirmed {
		return errEnvDeleteCancelled
	}
	return nil
}

// Execute deletes the environment from the application by:
// 1. Deleting the cloudformation stack.
// 2. Deleting the EnvManagerRole and CFNExecutionRole.
// 3. Deleting the parameter from the SSM store.
// The environment is removed from the store only if other delete operations succeed.
// Execute assumes that Validate is invoked first.
func (o *deleteEnvOpts) Execute() error {
	if err := o.initRuntimeClients(o); err != nil {
		return err
	}
	if err := o.validateNoRunningServices(); err != nil {
		return err
	}

	o.prog.Start(fmt.Sprintf(fmtDeleteEnvStart, o.name, o.appName))
	if err := o.ensureRolesAreRetained(); err != nil {
		o.prog.Stop(log.Serrorf(fmtDeleteEnvFailed, o.name, o.appName))
		return err
	}
	if err := o.deleteStack(); err != nil {
		o.prog.Stop(log.Serrorf(fmtDeleteEnvFailed, o.name, o.appName))
		return err
	}
	if err := o.deleteRoles(); err != nil {
		o.prog.Stop(log.Serrorf(fmtDeleteEnvFailed, o.name, o.appName))
		return err
	}
	// Only remove from SSM if the stack and roles were deleted. Otherwise, the command will error when re-run.
	if err := o.deleteFromStore(); err != nil {
		o.prog.Stop(log.Serrorf(fmtDeleteEnvFailed, o.name, o.appName))
		return err
	}
	o.prog.Stop(log.Ssuccessf(fmtDeleteEnvComplete, o.name, o.appName))
	return nil
}

// RecommendedActions is a no-op for this command.
func (o *deleteEnvOpts) RecommendedActions() []string {
	return nil
}

func (o *deleteEnvOpts) validateEnvName() error {
	if _, err := o.getEnvConfig(); err != nil {
		return err
	}
	return nil
}

func (o *deleteEnvOpts) askEnvName() error {
	if o.name != "" {
		return nil
	}
	env, err := o.sel.Environment(envDeleteNamePrompt, "", o.appName)
	if err != nil {
		return fmt.Errorf("select environment to delete: %w", err)
	}
	o.name = env
	return nil
}

func (o *deleteEnvOpts) validateNoRunningServices() error {
	stacks, err := o.rg.GetResources(&resourcegroupstaggingapi.GetResourcesInput{
		ResourceTypeFilters: []*string{aws.String("cloudformation")},
		TagFilters: []*resourcegroupstaggingapi.TagFilter{
			{
				Key:    aws.String(deploy.ServiceTagKey),
				Values: []*string{}, // Matches any service stack.
			},
			{
				Key:    aws.String(deploy.EnvTagKey),
				Values: []*string{aws.String(o.name)},
			},
			{
				Key:    aws.String(deploy.AppTagKey),
				Values: []*string{aws.String(o.appName)},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("find service cloudformation stacks: %w", err)
	}
	if len(stacks.ResourceTagMappingList) > 0 {
		var svcNames []string
		for _, cfnStack := range stacks.ResourceTagMappingList {
			for _, t := range cfnStack.Tags {
				if *t.Key != deploy.ServiceTagKey {
					continue
				}
				svcNames = append(svcNames, *t.Value)
			}
		}
		return fmt.Errorf("service '%s' still exist within the environment %s", strings.Join(svcNames, ", "), o.name)
	}
	return nil
}

// ensureRolesAreRetained guarantees that the CloudformationExecutionRole and the EnvironmentManagerRole
// are retained when the environment cloudformation stack is deleted.
//
// This method is needed because the environment stack is deleted using the CloudformationExecutionRole which means
// that the role has to be retained in order for us to delete the stack. Similarly, only the EnvironmentManagerRole
// has permissions to delete the CloudformationExecutionRole so it must be retained.
// In earlier versions of the CLI, pre-commit 7e5428a, environment stacks were created without these roles retained.
// In case we encounter a legacy stack, we need to first update the stack to make sure these roles are retained and then
// proceed with the regular flow.
func (o *deleteEnvOpts) ensureRolesAreRetained() error {
	body, err := o.deployer.EnvironmentTemplate(o.appName, o.name)
	if err != nil {
		var stackDoesNotExist *awscfn.ErrStackNotFound
		if errors.As(err, &stackDoesNotExist) {
			return nil
		}
		return fmt.Errorf("get template body for environment %s in application %s: %v", o.name, o.appName, err)
	}
	// Check if the roles are already being retained.
	retainsExecRole := strings.Contains(body, `
  CloudformationExecutionRole:
    DeletionPolicy: Retain`)
	retainsManagerRole := strings.Contains(body, `
  EnvironmentManagerRole:
    DeletionPolicy: Retain`)
	if retainsExecRole && retainsManagerRole {
		// Nothing to do, this is **not** a legacy environment stack. Exit successfully.
		return nil
	}

	// Otherwise, update the body with the new deletion policies.
	newBody := body
	if !retainsExecRole {
		parts := strings.Split(newBody, "  CloudformationExecutionRole:\n")
		newBody = parts[0] + "  CloudformationExecutionRole:\n    DeletionPolicy: Retain\n" + parts[1]
	}
	if !retainsManagerRole {
		parts := strings.Split(newBody, "  EnvironmentManagerRole:\n")
		newBody = parts[0] + "  EnvironmentManagerRole:\n    DeletionPolicy: Retain\n" + parts[1]
	}

	env, err := o.getEnvConfig()
	if err != nil {
		return err
	}
	if err := o.deployer.UpdateEnvironmentTemplate(o.appName, o.name, newBody, env.ExecutionRoleARN); err != nil {
		return fmt.Errorf("update environment stack to retain environment roles: %w", err)
	}
	return nil
}

// deleteStack returns nil if the stack was deleted successfully. Otherwise, returns the error.
func (o *deleteEnvOpts) deleteStack() error {
	env, err := o.getEnvConfig()
	if err != nil {
		return err
	}
	if err := o.deployer.DeleteEnvironment(o.appName, o.name, env.ExecutionRoleARN); err != nil {
		return fmt.Errorf("delete environment %s stack: %w", o.name, err)
	}
	return nil
}

func (o *deleteEnvOpts) deleteRoles() error {
	env, err := o.getEnvConfig()
	if err != nil {
		return err
	}
	if err := o.iam.DeleteRole(env.ExecutionRoleARN); err != nil {
		return fmt.Errorf("delete role %s: %w", env.ExecutionRoleARN, err)
	}
	if err := o.iam.DeleteRole(env.ManagerRoleARN); err != nil {
		return fmt.Errorf("delete role %s: %w", env.ManagerRoleARN, err)
	}
	return nil
}

func (o *deleteEnvOpts) deleteFromStore() error {
	if err := o.store.DeleteEnvironment(o.appName, o.name); err != nil {
		return fmt.Errorf("delete environment %s configuration from application %s", o.name, o.appName)
	}
	return nil
}

func (o *deleteEnvOpts) getEnvConfig() (*config.Environment, error) {
	if o.envConfig != nil {
		// Already fetched once, return.
		return o.envConfig, nil
	}
	env, err := o.store.GetEnvironment(o.appName, o.name)
	if err != nil {
		return nil, fmt.Errorf("get environment %s configuration from app %s: %v", o.name, o.appName, err)
	}
	o.envConfig = env
	return env, nil
}

// buildEnvDeleteCmd builds the command to delete environment(s).
func buildEnvDeleteCmd() *cobra.Command {
	vars := deleteEnvVars{}
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Deletes an environment from your application.",
		Example: `
  Delete the "test" environment.
  /code $ copilot env delete --name test

  Delete the "test" environment without prompting.
  /code $ copilot env delete --name test --yes`,
		RunE: runCmdE(func(cmd *cobra.Command, args []string) error {
			opts, err := newDeleteEnvOpts(vars)
			if err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.Ask(); err != nil {
				return err
			}
			return opts.Execute()
		}),
	}
	cmd.Flags().StringVarP(&vars.appName, appFlag, appFlagShort, tryReadingAppName(), appFlagDescription)
	cmd.Flags().StringVarP(&vars.name, nameFlag, nameFlagShort, "", envFlagDescription)
	cmd.Flags().BoolVar(&vars.skipConfirmation, yesFlag, false, yesFlagDescription)
	return cmd
}
