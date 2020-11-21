// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/resourcegroupstaggingapi"
	"github.com/aws/copilot-cli/internal/pkg/cli/mocks"
	"github.com/aws/copilot-cli/internal/pkg/config"
	"github.com/aws/copilot-cli/internal/pkg/deploy"
	"github.com/aws/copilot-cli/internal/pkg/term/log"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

var noopInitRuntimeClients = func(opts *deleteEnvOpts) error {
	return nil
}

func TestDeleteEnvOpts_Validate(t *testing.T) {
	const (
		testAppName = "phonetool"
		testEnvName = "test"
	)
	testCases := map[string]struct {
		inAppName string
		inEnv     string
		mockStore func(ctrl *gomock.Controller) *mocks.MockenvironmentStore

		wantedError error
	}{
		"failed to retrieve environment from store": {
			inAppName: testAppName,
			inEnv:     testEnvName,
			mockStore: func(ctrl *gomock.Controller) *mocks.MockenvironmentStore {
				envStore := mocks.NewMockenvironmentStore(ctrl)
				envStore.EXPECT().GetEnvironment(testAppName, testEnvName).Return(nil, errors.New("some error"))
				return envStore
			},
			wantedError: errors.New("get environment test configuration from app phonetool: some error"),
		},
		"environment exists": {
			inAppName: testAppName,
			inEnv:     testEnvName,
			mockStore: func(ctrl *gomock.Controller) *mocks.MockenvironmentStore {
				envStore := mocks.NewMockenvironmentStore(ctrl)
				envStore.EXPECT().GetEnvironment(testAppName, testEnvName).Return(&config.Environment{}, nil)
				return envStore
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			opts := &deleteEnvOpts{
				deleteEnvVars: deleteEnvVars{
					name:    tc.inEnv,
					appName: tc.inAppName,
				},
				store: tc.mockStore(ctrl),
			}

			// WHEN
			err := opts.Validate()

			// THEN
			if tc.wantedError != nil {
				require.EqualError(t, err, tc.wantedError.Error())
			}
		})
	}
}

func TestDeleteEnvOpts_Ask(t *testing.T) {
	const (
		testApp = "phonetool"
		testEnv = "test"
	)
	testCases := map[string]struct {
		inEnvName          string
		inSkipConfirmation bool

		mockDependencies func(ctrl *gomock.Controller, o *deleteEnvOpts)

		wantedEnvName string
		wantedError   error
	}{
		"prompts for all required flags": {
			inSkipConfirmation: false,
			mockDependencies: func(ctrl *gomock.Controller, o *deleteEnvOpts) {
				mockSelector := mocks.NewMockconfigSelector(ctrl)
				mockSelector.EXPECT().Environment(envDeleteNamePrompt, "", testApp).Return(testEnv, nil)

				mockPrompter := mocks.NewMockprompter(ctrl)
				mockPrompter.EXPECT().Confirm(fmt.Sprintf(fmtDeleteEnvPrompt, testEnv, testApp), gomock.Any()).Return(true, nil)

				o.sel = mockSelector
				o.prompt = mockPrompter
			},
			wantedEnvName: testEnv,
		},
		"wraps error from prompting for confirmation": {
			inSkipConfirmation: false,
			inEnvName:          testEnv,
			mockDependencies: func(ctrl *gomock.Controller, o *deleteEnvOpts) {

				mockPrompter := mocks.NewMockprompter(ctrl)
				mockPrompter.EXPECT().Confirm(fmt.Sprintf(fmtDeleteEnvPrompt, testEnv, testApp), gomock.Any()).Return(false, errors.New("some error"))

				o.prompt = mockPrompter
			},

			wantedError: errors.New("confirm to delete environment test: some error"),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			opts := &deleteEnvOpts{
				deleteEnvVars: deleteEnvVars{
					name:             tc.inEnvName,
					appName:          testApp,
					skipConfirmation: tc.inSkipConfirmation,
				},
			}
			tc.mockDependencies(ctrl, opts)

			// WHEN
			err := opts.Ask()

			// THEN
			if tc.wantedError == nil {
				require.Equal(t, tc.wantedEnvName, opts.name)
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.wantedError.Error())
			}
		})
	}
}

func TestDeleteEnvOpts_Execute(t *testing.T) {
	testCases := map[string]struct {
		given func(t *testing.T, ctrl *gomock.Controller) *deleteEnvOpts

		mockRG     func(ctrl *gomock.Controller) *mocks.MockresourceGetter
		mockProg   func(ctrl *gomock.Controller) *mocks.Mockprogress
		mockDeploy func(ctrl *gomock.Controller) *mocks.MockenvironmentDeployer
		mockStore  func(ctrl *gomock.Controller) *mocks.MockenvironmentStore

		wantedError error
	}{
		"returns wrapped errors when failed to retrieve running services in the environment": {
			given: func(t *testing.T, ctrl *gomock.Controller) *deleteEnvOpts {
				m := mocks.NewMockresourceGetter(ctrl)
				m.EXPECT().GetResources(gomock.Any()).Return(nil, errors.New("some error"))

				return &deleteEnvOpts{
					rg:                 m,
					initRuntimeClients: noopInitRuntimeClients,
				}
			},
			wantedError: errors.New("find service cloudformation stacks: some error"),
		},
		"returns error when there are running services": {
			given: func(t *testing.T, ctrl *gomock.Controller) *deleteEnvOpts {
				m := mocks.NewMockresourceGetter(ctrl)
				m.EXPECT().GetResources(gomock.Any()).Return(&resourcegroupstaggingapi.GetResourcesOutput{
					ResourceTagMappingList: []*resourcegroupstaggingapi.ResourceTagMapping{
						{
							Tags: []*resourcegroupstaggingapi.Tag{
								{
									Key:   aws.String(deploy.ServiceTagKey),
									Value: aws.String("frontend"),
								},
								{
									Key:   aws.String(deploy.ServiceTagKey),
									Value: aws.String("backend"),
								},
							},
						},
					},
				}, nil)

				return &deleteEnvOpts{
					deleteEnvVars: deleteEnvVars{
						appName: "phonetool",
						name:    "test",
					},
					rg:                 m,
					initRuntimeClients: noopInitRuntimeClients,
				}
			},

			wantedError: errors.New("service 'frontend, backend' still exist within the environment test"),
		},
		"returns wrapped error when environment stack cannot be updated to retain roles": {
			given: func(t *testing.T, ctrl *gomock.Controller) *deleteEnvOpts {
				rg := mocks.NewMockresourceGetter(ctrl)
				rg.EXPECT().GetResources(gomock.Any()).Return(&resourcegroupstaggingapi.GetResourcesOutput{
					ResourceTagMappingList: []*resourcegroupstaggingapi.ResourceTagMapping{}}, nil)

				prog := mocks.NewMockprogress(ctrl)
				prog.EXPECT().Start(gomock.Any())

				deployer := mocks.NewMockenvironmentDeployer(ctrl)
				deployer.EXPECT().EnvironmentTemplate(gomock.Any(), gomock.Any()).Return(`
  CloudformationExecutionRole:
  EnvironmentManagerRole:
`, nil)
				deployer.EXPECT().UpdateEnvironmentTemplate(
					"phonetool",
					"test",
					`
  CloudformationExecutionRole:
    DeletionPolicy: Retain
  EnvironmentManagerRole:
    DeletionPolicy: Retain
`, "arn").Return(errors.New("some error"))

				prog.EXPECT().Stop(log.Serror("Failed to delete environment test from application phonetool.\n"))

				return &deleteEnvOpts{
					deleteEnvVars: deleteEnvVars{
						appName: "phonetool",
						name:    "test",
					},
					rg:       rg,
					deployer: deployer,
					prog:     prog,
					envConfig: &config.Environment{
						ExecutionRoleARN: "arn",
					},
					initRuntimeClients: noopInitRuntimeClients,
				}
			},
			wantedError: errors.New("update environment stack to retain environment roles: some error"),
		},
		"returns wrapped error when stack cannot be deleted": {
			given: func(t *testing.T, ctrl *gomock.Controller) *deleteEnvOpts {
				rg := mocks.NewMockresourceGetter(ctrl)
				rg.EXPECT().GetResources(gomock.Any()).Return(&resourcegroupstaggingapi.GetResourcesOutput{
					ResourceTagMappingList: []*resourcegroupstaggingapi.ResourceTagMapping{}}, nil)

				prog := mocks.NewMockprogress(ctrl)
				prog.EXPECT().Start(gomock.Any())

				deployer := mocks.NewMockenvironmentDeployer(ctrl)
				deployer.EXPECT().EnvironmentTemplate(gomock.Any(), gomock.Any()).Return(`
  CloudformationExecutionRole:
    DeletionPolicy: Retain
  EnvironmentManagerRole:
    DeletionPolicy: Retain`, nil)
				deployer.EXPECT().DeleteEnvironment(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("some error"))

				prog.EXPECT().Stop(log.Serror("Failed to delete environment test from application phonetool.\n"))

				return &deleteEnvOpts{
					deleteEnvVars: deleteEnvVars{
						appName: "phonetool",
						name:    "test",
					},
					rg:                 rg,
					deployer:           deployer,
					prog:               prog,
					envConfig:          &config.Environment{},
					initRuntimeClients: noopInitRuntimeClients,
				}
			},

			wantedError: errors.New("delete environment test stack: some error"),
		},
		"returns wrapped error when role cannot be deleted": {
			given: func(t *testing.T, ctrl *gomock.Controller) *deleteEnvOpts {
				rg := mocks.NewMockresourceGetter(ctrl)
				rg.EXPECT().GetResources(gomock.Any()).Return(&resourcegroupstaggingapi.GetResourcesOutput{
					ResourceTagMappingList: []*resourcegroupstaggingapi.ResourceTagMapping{}}, nil)

				prog := mocks.NewMockprogress(ctrl)
				prog.EXPECT().Start(gomock.Any())

				deployer := mocks.NewMockenvironmentDeployer(ctrl)
				deployer.EXPECT().EnvironmentTemplate(gomock.Any(), gomock.Any()).Return(`
  CloudformationExecutionRole:
    DeletionPolicy: Retain
  EnvironmentManagerRole:
    DeletionPolicy: Retain`, nil)
				deployer.EXPECT().DeleteEnvironment(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

				iam := mocks.NewMockroleDeleter(ctrl)
				gomock.InOrder(
					iam.EXPECT().DeleteRole("execARN").Return(nil),
					iam.EXPECT().DeleteRole("managerRoleARN").Return(errors.New("some error")),
				)

				prog.EXPECT().Stop(log.Serror("Failed to delete environment test from application phonetool.\n"))

				return &deleteEnvOpts{
					deleteEnvVars: deleteEnvVars{
						appName: "phonetool",
						name:    "test",
					},
					rg:       rg,
					deployer: deployer,
					prog:     prog,
					iam:      iam,
					envConfig: &config.Environment{
						ExecutionRoleARN: "execARN",
						ManagerRoleARN:   "managerRoleARN",
					},
					initRuntimeClients: noopInitRuntimeClients,
				}
			},
			wantedError: errors.New("delete role managerRoleARN: some error"),
		},
		"deletes the stack, then the roles, then SSM by default": {
			given: func(t *testing.T, ctrl *gomock.Controller) *deleteEnvOpts {
				rg := mocks.NewMockresourceGetter(ctrl)
				rg.EXPECT().GetResources(gomock.Any()).Return(&resourcegroupstaggingapi.GetResourcesOutput{
					ResourceTagMappingList: []*resourcegroupstaggingapi.ResourceTagMapping{}}, nil)

				prog := mocks.NewMockprogress(ctrl)
				prog.EXPECT().Start("Deleting environment test from application phonetool.")

				deployer := mocks.NewMockenvironmentDeployer(ctrl)
				deployer.EXPECT().EnvironmentTemplate("phonetool", "test").Return(`
  CloudformationExecutionRole:
    DeletionPolicy: Retain
  EnvironmentManagerRole:
    DeletionPolicy: Retain`, nil)
				deployer.EXPECT().DeleteEnvironment("phonetool", "test", "execARN").Return(nil)

				iam := mocks.NewMockroleDeleter(ctrl)
				iam.EXPECT().DeleteRole("execARN").Return(nil)
				iam.EXPECT().DeleteRole("managerRoleARN").Return(nil)

				store := mocks.NewMockenvironmentStore(ctrl)
				store.EXPECT().DeleteEnvironment("phonetool", "test").Return(nil)

				prog.EXPECT().Stop(log.Ssuccess("Deleted environment test from application phonetool.\n"))

				return &deleteEnvOpts{
					deleteEnvVars: deleteEnvVars{
						appName: "phonetool",
						name:    "test",
					},
					rg:       rg,
					deployer: deployer,
					prog:     prog,
					iam:      iam,
					store:    store,
					envConfig: &config.Environment{
						ExecutionRoleARN: "execARN",
						ManagerRoleARN:   "managerRoleARN",
					},
					initRuntimeClients: noopInitRuntimeClients,
				}
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			opts := tc.given(t, ctrl)

			// WHEN
			err := opts.Execute()

			// THEN
			if tc.wantedError != nil {
				require.EqualError(t, err, tc.wantedError.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}
