// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package selector

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/copilot-cli/internal/pkg/config"
	"github.com/aws/copilot-cli/internal/pkg/term/prompt"
	"github.com/aws/copilot-cli/internal/pkg/term/selector/mocks"
	"github.com/aws/copilot-cli/internal/pkg/workspace"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

type deploySelectMocks struct {
	deploySvc *mocks.MockDeployStoreClient
	configSvc *mocks.MockConfigLister
	prompt    *mocks.MockPrompter
}

func TestDeploySelect_Service(t *testing.T) {
	const testApp = "mockApp"
	testCases := map[string]struct {
		setupMocks func(mocks deploySelectMocks)
		svc        string
		env        string

		wantErr error
		wantEnv string
		wantSvc string
	}{
		"return error if fail to retrieve environment": {
			setupMocks: func(m deploySelectMocks) {
				m.configSvc.
					EXPECT().
					ListEnvironments(testApp).
					Return(nil, errors.New("some error"))

			},
			wantErr: fmt.Errorf("list environments: list environments: some error"),
		},
		"return error if fail to list deployed services": {
			setupMocks: func(m deploySelectMocks) {
				m.configSvc.
					EXPECT().
					ListEnvironments(testApp).
					Return([]*config.Environment{
						{
							Name: "test",
						},
					}, nil)

				m.deploySvc.
					EXPECT().
					ListDeployedServices(testApp, "test").
					Return(nil, errors.New("some error"))
			},
			wantErr: fmt.Errorf("list deployed service for environment test: some error"),
		},
		"return error if no deployed services found": {
			setupMocks: func(m deploySelectMocks) {
				m.configSvc.
					EXPECT().
					ListEnvironments(testApp).
					Return([]*config.Environment{
						{
							Name: "test",
						},
					}, nil)

				m.deploySvc.
					EXPECT().
					ListDeployedServices(testApp, "test").
					Return([]string{}, nil)
			},
			wantErr: fmt.Errorf("no deployed services found in application %s", testApp),
		},
		"return error if fail to select": {
			setupMocks: func(m deploySelectMocks) {
				m.configSvc.
					EXPECT().
					ListEnvironments(testApp).
					Return([]*config.Environment{
						{
							Name: "test",
						},
					}, nil)

				m.deploySvc.
					EXPECT().
					ListDeployedServices(testApp, "test").
					Return([]string{"mockSvc1", "mockSvc2"}, nil)

				m.prompt.
					EXPECT().
					SelectOne("Select a deployed service", "Help text", []string{"mockSvc1 (test)", "mockSvc2 (test)"}).
					Return("", errors.New("some error"))
			},
			wantErr: fmt.Errorf("select deployed services for application %s: some error", testApp),
		},
		"success": {
			setupMocks: func(m deploySelectMocks) {
				m.configSvc.
					EXPECT().
					ListEnvironments(testApp).
					Return([]*config.Environment{
						{
							Name: "test",
						},
					}, nil)

				m.deploySvc.
					EXPECT().
					ListDeployedServices(testApp, "test").
					Return([]string{"mockSvc1", "mockSvc2"}, nil)

				m.prompt.
					EXPECT().
					SelectOne("Select a deployed service", "Help text", []string{"mockSvc1 (test)", "mockSvc2 (test)"}).
					Return("mockSvc1 (test)", nil)
			},
			wantEnv: "test",
			wantSvc: "mockSvc1",
		},
		"skip with only one deployed service": {
			setupMocks: func(m deploySelectMocks) {
				m.configSvc.
					EXPECT().
					ListEnvironments(testApp).
					Return([]*config.Environment{
						{
							Name: "test",
						},
					}, nil)

				m.deploySvc.
					EXPECT().
					ListDeployedServices(testApp, "test").
					Return([]string{"mockSvc"}, nil)
			},
			wantEnv: "test",
			wantSvc: "mockSvc",
		},
		"return error if fail to check if service passed in by flag is deployed or not": {
			env: "test",
			svc: "mockSvc",
			setupMocks: func(m deploySelectMocks) {
				m.deploySvc.
					EXPECT().
					IsServiceDeployed(testApp, "test", "mockSvc").
					Return(false, errors.New("some error"))
			},
			wantErr: fmt.Errorf("check if service mockSvc is deployed in environment test: some error"),
		},
		"success with flags": {
			env: "test",
			svc: "mockSvc",
			setupMocks: func(m deploySelectMocks) {
				m.deploySvc.
					EXPECT().
					IsServiceDeployed(testApp, "test", "mockSvc").
					Return(true, nil)
			},
			wantEnv: "test",
			wantSvc: "mockSvc",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockdeploySvc := mocks.NewMockDeployStoreClient(ctrl)
			mockconfigSvc := mocks.NewMockConfigLister(ctrl)
			mockprompt := mocks.NewMockPrompter(ctrl)
			mocks := deploySelectMocks{
				deploySvc: mockdeploySvc,
				configSvc: mockconfigSvc,
				prompt:    mockprompt,
			}
			tc.setupMocks(mocks)

			sel := DeploySelect{
				Select: &Select{
					config: mockconfigSvc,
					prompt: mockprompt,
				},
				deployStoreSvc: mockdeploySvc,
			}
			gotDeployed, err := sel.DeployedService("Select a deployed service", "Help text", testApp, WithEnv(tc.env), WithSvc(tc.svc))
			if tc.wantErr != nil {
				require.EqualError(t, tc.wantErr, err.Error())
			} else {
				require.Equal(t, tc.wantSvc, gotDeployed.Svc)
				require.Equal(t, tc.wantEnv, gotDeployed.Env)
			}
		})
	}
}

type workspaceSelectMocks struct {
	workloadLister *mocks.MockWorkspaceRetriever
	prompt         *mocks.MockPrompter
	configLister   *mocks.MockConfigLister
}

func TestWorkspaceSelect_Service(t *testing.T) {
	testCases := map[string]struct {
		setupMocks func(mocks workspaceSelectMocks)
		wantErr    error
		want       string
	}{
		"with no workspace services and no store services": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.EXPECT().ServiceNames().Return(
					[]string{}, nil).Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListServices("app-name").Return(
					[]*config.Workload{}, nil).Times(1)
				m.prompt.EXPECT().SelectOne(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			},
			wantErr: fmt.Errorf("no services found"),
		},
		"with one workspace service but no store services": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.EXPECT().ServiceNames().Return(
					[]string{
						"service1",
					}, nil).
					Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListServices("app-name").Return(
					[]*config.Workload{}, nil).Times(1)
			},
			wantErr: fmt.Errorf("no services found"),
		},
		"with one store service but no workspace services": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.EXPECT().ServiceNames().Return(
					[]string{}, nil).
					Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListServices("app-name").Return(
					[]*config.Workload{
						{
							App:  "app-name",
							Name: "service1",
							Type: "load balanced web service",
						},
					}, nil).Times(1)
			},
			wantErr: fmt.Errorf("no services found"),
		},
		"with only one service in both workspace and store (skips prompting)": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.EXPECT().ServiceNames().Return(
					[]string{
						"service1",
					}, nil).Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListServices("app-name").Return(
					[]*config.Workload{
						{
							App:  "app-name",
							Name: "service1",
							Type: "load balanced web service",
						},
					}, nil).Times(1)
				m.prompt.
					EXPECT().SelectOne(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			},
			want: "service1",
		},
		"with multiple workspace services but only one store service (skips prompting)": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.EXPECT().ServiceNames().Return(
					[]string{
						"service1",
						"service2",
						"service3",
					}, nil).Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListServices("app-name").Return(
					[]*config.Workload{
						{
							App:  "app-name",
							Name: "service1",
							Type: "load balanced web service",
						},
					}, nil).Times(1)
				m.prompt.
					EXPECT().SelectOne(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			},
			want: "service1",
		},
		"with multiple store services but only one workspace service (skips prompting)": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.EXPECT().ServiceNames().Return(
					[]string{
						"service3",
					}, nil).Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListServices("app-name").Return(
					[]*config.Workload{
						{
							App:  "app-name",
							Name: "service1",
							Type: "load balanced web service",
						},
						{
							App:  "app-name",
							Name: "service2",
							Type: "load balanced web service",
						},
						{
							App:  "app-name",
							Name: "service3",
							Type: "load balanced web service",
						},
					}, nil).Times(1)
				m.prompt.
					EXPECT().SelectOne(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			},
			want: "service3",
		},
		"with multiple workspace services and multiple store services, of which multiple overlap": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.
					EXPECT().ServiceNames().Return(
					[]string{
						"service1",
						"service2",
						"service3",
					}, nil).Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListServices("app-name").Return(
					[]*config.Workload{
						{
							App:  "app-name",
							Name: "service2",
							Type: "load balanced web service",
						},
						{
							App:  "app-name",
							Name: "service3",
							Type: "load balanced web service",
						},
						{
							App:  "app-name",
							Name: "service4",
							Type: "load balanced web service",
						},
					}, nil).Times(1)
				m.prompt.
					EXPECT().
					SelectOne(
						gomock.Eq("Select a service"),
						gomock.Eq("Help text"),
						gomock.Eq([]string{"service2", "service3"}),
						gomock.Any()).
					Return("service2", nil).Times(1)
			},
			want: "service2",
		},
		"with error retrieving services from workspace": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.
					EXPECT().ServiceNames().Return(
					[]string{""}, errors.New("some error"))
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
			},
			wantErr: errors.New("retrieve services from workspace: some error"),
		},
		"with error retrieving services from store": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.EXPECT().ServiceNames().Return(
					[]string{
						"service1",
						"service2",
					}, nil).
					Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListServices("app-name").Return(
					nil, errors.New("some error"))
			},
			wantErr: errors.New("retrieve services from store: some error"),
		},
		"with error selecting services": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.
					EXPECT().ServiceNames().Return(
					[]string{
						"service1",
						"service2",
					}, nil).
					Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListServices("app-name").Return(
					[]*config.Workload{
						{
							App:  "app-name",
							Name: "service1",
							Type: "load balanced web service",
						},
						{
							App:  "app-name",
							Name: "service2",
							Type: "load balanced web service",
						},
					}, nil).Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Eq([]string{"service1", "service2"}), gomock.Any()).
					Return("", fmt.Errorf("error selecting")).
					Times(1)
			},
			wantErr: fmt.Errorf("select service: error selecting"),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockwsRetriever := mocks.NewMockWorkspaceRetriever(ctrl)
			mockconfigLister := mocks.NewMockConfigLister(ctrl)
			mockprompt := mocks.NewMockPrompter(ctrl)
			mocks := workspaceSelectMocks{
				workloadLister: mockwsRetriever,
				configLister:   mockconfigLister,
				prompt:         mockprompt,
			}
			tc.setupMocks(mocks)

			sel := WorkspaceSelect{
				Select: &Select{
					prompt: mockprompt,
					config: mockconfigLister,
				},
				ws: mockwsRetriever,
			}
			got, err := sel.Service("Select a service", "Help text")
			if tc.wantErr != nil {
				require.EqualError(t, tc.wantErr, err.Error())
			} else {
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestWorkspaceSelect_Job(t *testing.T) {
	testCases := map[string]struct {
		setupMocks func(mocks workspaceSelectMocks)
		wantErr    error
		want       string
	}{
		"with no workspace jobs and no store jobs": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.
					EXPECT().JobNames().Return(
					[]string{}, nil).Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListJobs("app-name").Return(
					[]*config.Workload{}, nil).Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			},
			wantErr: fmt.Errorf("no jobs found"),
		},
		"with one workspace job but no store jobs": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.
					EXPECT().JobNames().Return(
					[]string{
						"job1",
					}, nil).Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListJobs("app-name").Return(
					[]*config.Workload{}, nil).Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			},
			wantErr: fmt.Errorf("no jobs found"),
		},
		"with one store job but no workspace jobs": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.
					EXPECT().JobNames().Return(
					[]string{}, nil).Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListJobs("app-name").Return(
					[]*config.Workload{
						{
							App:  "app-name",
							Name: "job1",
							Type: "Scheduled Job",
						},
					}, nil).Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			},
			wantErr: fmt.Errorf("no jobs found"),
		},
		"with only one in both workspace and store (skips prompting)": {
			setupMocks: func(m workspaceSelectMocks) {

				m.workloadLister.
					EXPECT().JobNames().Return(
					[]string{
						"resizer",
					}, nil).Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListJobs("app-name").Return(
					[]*config.Workload{
						{
							App:  "app-name",
							Name: "resizer",
							Type: "Scheduled Job",
						},
					}, nil).Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			},
			want: "resizer",
		},
		"with multiple workspace jobs but only one store job (skips prompting)": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.EXPECT().JobNames().Return(
					[]string{
						"job1",
						"job2",
						"job3",
					}, nil).Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListJobs("app-name").Return(
					[]*config.Workload{
						{
							App:  "app-name",
							Name: "job2",
							Type: "Scheduled Job",
						},
					}, nil).Times(1)
				m.prompt.
					EXPECT().SelectOne(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			},
			want: "job2",
		},
		"with multiple store jobs but only one workspace job (skips prompting)": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.EXPECT().JobNames().Return(
					[]string{
						"job3",
					}, nil).Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListJobs("app-name").Return(
					[]*config.Workload{
						{
							App:  "app-name",
							Name: "job1",
							Type: "Scheduled Job",
						},
						{
							App:  "app-name",
							Name: "job2",
							Type: "Scheduled Job",
						},
						{
							App:  "app-name",
							Name: "job3",
							Type: "Scheduled Job",
						},
					}, nil).Times(1)
				m.prompt.
					EXPECT().SelectOne(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			},
			want: "job3",
		},
		"with multiple workspace jobs and multiple store jobs, of which multiple overlap": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.EXPECT().JobNames().Return(
					[]string{
						"job1",
						"job2",
						"job3",
					}, nil).Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.
					EXPECT().
					ListJobs("app-name").
					Return(
						[]*config.Workload{
							{
								App:  "app-name",
								Name: "job2",
								Type: "Scheduled Job",
							},
							{
								App:  "app-name",
								Name: "job3",
								Type: "Scheduled Job",
							},
							{
								App:  "app-name",
								Name: "job4",
								Type: "Scheduled Job",
							},
						}, nil).Times(1)
				m.prompt.
					EXPECT().
					SelectOne(
						gomock.Eq("Select a job"),
						gomock.Eq("Help text"),
						gomock.Eq([]string{"job2", "job3"}),
						gomock.Any()).
					Return("job2", nil).
					Times(1)
			},
			want: "job2",
		},
		"with error retrieving jobs from workspace": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.
					EXPECT().JobNames().Return(
					[]string{""}, errors.New("some error"))
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
			},
			wantErr: errors.New("retrieve jobs from workspace: some error"),
		},
		"with error retrieving jobs from store": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.EXPECT().JobNames().Return(
					[]string{
						"service1",
						"service2",
					}, nil).
					Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListJobs("app-name").Return(
					nil, errors.New("some error"))
			},
			wantErr: errors.New("retrieve jobs from store: some error"),
		},
		"with error selecting jobs": {
			setupMocks: func(m workspaceSelectMocks) {
				m.workloadLister.EXPECT().JobNames().Return(
					[]string{
						"resizer1",
						"resizer2",
					}, nil).Times(1)
				m.workloadLister.EXPECT().Summary().Return(
					&workspace.Summary{
						Application: "app-name",
					}, nil)
				m.configLister.EXPECT().ListJobs("app-name").Return(
					[]*config.Workload{
						{
							App:  "app-name",
							Name: "resizer1",
							Type: "Scheduled Job",
						},
						{
							App:  "app-name",
							Name: "resizer2",
							Type: "Scheduled Job",
						},
					}, nil).Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Eq([]string{"resizer1", "resizer2"}), gomock.Any()).
					Return("", fmt.Errorf("error selecting")).
					Times(1)
			},
			wantErr: fmt.Errorf("select job: error selecting"),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockwsRetriever := mocks.NewMockWorkspaceRetriever(ctrl)
			mockconfigLister := mocks.NewMockConfigLister(ctrl)
			mockprompt := mocks.NewMockPrompter(ctrl)
			mocks := workspaceSelectMocks{
				workloadLister: mockwsRetriever,
				configLister:   mockconfigLister,
				prompt:         mockprompt,
			}
			tc.setupMocks(mocks)

			sel := WorkspaceSelect{
				Select: &Select{
					prompt: mockprompt,
					config: mockconfigLister,
				},
				ws: mockwsRetriever,
			}
			got, err := sel.Job("Select a job", "Help text")
			if tc.wantErr != nil {
				require.EqualError(t, tc.wantErr, err.Error())
			} else {
				require.Equal(t, tc.want, got)
			}
		})
	}
}

type configSelectMocks struct {
	serviceLister *mocks.MockConfigLister
	prompt        *mocks.MockPrompter
}

func TestConfigSelect_Service(t *testing.T) {
	appName := "myapp"
	testCases := map[string]struct {
		setupMocks func(m configSelectMocks)
		wantErr    error
		want       string
	}{
		"with no services": {
			setupMocks: func(m configSelectMocks) {
				m.serviceLister.
					EXPECT().
					ListServices(gomock.Eq(appName)).
					Return([]*config.Workload{}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)

			},
			wantErr: fmt.Errorf("no services found in app myapp"),
		},
		"with only one service (skips prompting)": {
			setupMocks: func(m configSelectMocks) {
				m.serviceLister.
					EXPECT().
					ListServices(gomock.Eq(appName)).
					Return([]*config.Workload{
						{
							App:  appName,
							Name: "service1",
							Type: "load balanced web service",
						},
					}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)

			},
			want: "service1",
		},
		"with multiple services": {
			setupMocks: func(m configSelectMocks) {
				m.serviceLister.
					EXPECT().
					ListServices(gomock.Eq(appName)).
					Return([]*config.Workload{
						{
							App:  appName,
							Name: "service1",
							Type: "load balanced web service",
						},
						{
							App:  appName,
							Name: "service2",
							Type: "backend service",
						},
					}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(
						gomock.Eq("Select a service"),
						gomock.Eq("Help text"),
						gomock.Eq([]string{"service1", "service2"})).
					Return("service2", nil).
					Times(1)
			},
			want: "service2",
		},
		"with error selecting services": {
			setupMocks: func(m configSelectMocks) {
				m.serviceLister.
					EXPECT().
					ListServices(gomock.Eq(appName)).
					Return([]*config.Workload{
						{
							App:  appName,
							Name: "service1",
							Type: "load balanced web service",
						},
						{
							App:  appName,
							Name: "service2",
							Type: "backend service",
						},
					}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Eq([]string{"service1", "service2"})).
					Return("", fmt.Errorf("error selecting")).
					Times(1)
			},
			wantErr: fmt.Errorf("select service: error selecting"),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockconfigLister := mocks.NewMockConfigLister(ctrl)
			mockprompt := mocks.NewMockPrompter(ctrl)
			mocks := configSelectMocks{
				serviceLister: mockconfigLister,
				prompt:        mockprompt,
			}
			tc.setupMocks(mocks)

			sel := ConfigSelect{
				Select: &Select{
					prompt: mockprompt,
				},
				svcLister: mockconfigLister,
			}

			got, err := sel.Service("Select a service", "Help text", appName)
			if tc.wantErr != nil {
				require.EqualError(t, tc.wantErr, err.Error())
			} else {
				require.Equal(t, tc.want, got)
			}
		})
	}
}

type environmentMocks struct {
	envLister *mocks.MockConfigLister
	prompt    *mocks.MockPrompter
}

func TestSelect_Environment(t *testing.T) {
	appName := "myapp"
	additionalOpt1, additionalOpt2 := "opt1", "opt2"

	testCases := map[string]struct {
		inAdditionalOpts []string

		setupMocks func(m environmentMocks)
		wantErr    error
		want       string
	}{
		"with no environments": {
			setupMocks: func(m environmentMocks) {
				m.envLister.
					EXPECT().
					ListEnvironments(gomock.Eq(appName)).
					Return([]*config.Environment{}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)

			},
			wantErr: fmt.Errorf("no environments found in app myapp"),
		},
		"with only one environment (skips prompting)": {
			setupMocks: func(m environmentMocks) {
				m.envLister.
					EXPECT().
					ListEnvironments(gomock.Eq(appName)).
					Return([]*config.Environment{
						{
							App:  appName,
							Name: "env1",
						},
					}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)

			},
			want: "env1",
		},
		"with multiple environments": {
			setupMocks: func(m environmentMocks) {
				m.envLister.
					EXPECT().
					ListEnvironments(gomock.Eq(appName)).
					Return([]*config.Environment{
						{
							App:  appName,
							Name: "env1",
						},
						{
							App:  appName,
							Name: "env2",
						},
					}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(
						gomock.Eq("Select an environment"),
						gomock.Eq("Help text"),
						gomock.Eq([]string{"env1", "env2"})).
					Return("env2", nil).
					Times(1)
			},
			want: "env2",
		},
		"with error selecting environments": {
			setupMocks: func(m environmentMocks) {
				m.envLister.
					EXPECT().
					ListEnvironments(gomock.Eq(appName)).
					Return([]*config.Environment{
						{
							App:  appName,
							Name: "env1",
						},
						{
							App:  appName,
							Name: "env2",
						},
					}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Eq([]string{"env1", "env2"})).
					Return("", fmt.Errorf("error selecting")).
					Times(1)
			},
			wantErr: fmt.Errorf("select environment: error selecting"),
		},
		"no environment but with one additional option": {
			inAdditionalOpts: []string{additionalOpt1},
			setupMocks: func(m environmentMocks) {
				m.envLister.
					EXPECT().
					ListEnvironments(gomock.Eq(appName)).
					Return([]*config.Environment{}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)
			},

			want: additionalOpt1,
		},
		"no environment but with multiple additional options": {
			inAdditionalOpts: []string{additionalOpt1, additionalOpt2},
			setupMocks: func(m environmentMocks) {
				m.envLister.
					EXPECT().
					ListEnvironments(gomock.Eq(appName)).
					Return([]*config.Environment{}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), []string{additionalOpt1, additionalOpt2}).
					Times(1).
					Return(additionalOpt2, nil)
			},

			want: additionalOpt2,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockenvLister := mocks.NewMockConfigLister(ctrl)
			mockprompt := mocks.NewMockPrompter(ctrl)
			mocks := environmentMocks{
				envLister: mockenvLister,
				prompt:    mockprompt,
			}
			tc.setupMocks(mocks)

			sel := Select{
				prompt: mockprompt,
				config: mockenvLister,
			}

			got, err := sel.Environment("Select an environment", "Help text", appName, tc.inAdditionalOpts...)
			if tc.wantErr != nil {
				require.EqualError(t, tc.wantErr, err.Error())
			} else {
				require.Equal(t, tc.want, got)
			}
		})
	}
}

type applicationMocks struct {
	appLister *mocks.MockConfigLister
	prompt    *mocks.MockPrompter
}

func TestSelect_Application(t *testing.T) {
	testCases := map[string]struct {
		setupMocks func(m applicationMocks)
		wantErr    error
		want       string
	}{
		"with no apps": {
			setupMocks: func(m applicationMocks) {
				m.appLister.
					EXPECT().
					ListApplications().
					Return([]*config.Application{}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)

			},
			wantErr: fmt.Errorf("no apps found"),
		},
		"with only one app (skips prompting)": {
			setupMocks: func(m applicationMocks) {
				m.appLister.
					EXPECT().
					ListApplications().
					Return([]*config.Application{
						{
							Name: "app1",
						},
					}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Any()).
					Times(0)

			},
			want: "app1",
		},
		"with multiple apps": {
			setupMocks: func(m applicationMocks) {
				m.appLister.
					EXPECT().
					ListApplications().
					Return([]*config.Application{
						{
							Name: "app1",
						},
						{
							Name: "app2",
						},
					}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(
						gomock.Eq("Select an app"),
						gomock.Eq("Help text"),
						gomock.Eq([]string{"app1", "app2"})).
					Return("app2", nil).
					Times(1)
			},
			want: "app2",
		},
		"with error selecting apps": {
			setupMocks: func(m applicationMocks) {
				m.appLister.
					EXPECT().
					ListApplications().
					Return([]*config.Application{
						{
							Name: "app1",
						},
						{
							Name: "app2",
						},
					}, nil).
					Times(1)
				m.prompt.
					EXPECT().
					SelectOne(gomock.Any(), gomock.Any(), gomock.Eq([]string{"app1", "app2"})).
					Return("", fmt.Errorf("error selecting")).
					Times(1)
			},
			wantErr: fmt.Errorf("select application: error selecting"),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockappLister := mocks.NewMockConfigLister(ctrl)
			mockprompt := mocks.NewMockPrompter(ctrl)
			mocks := applicationMocks{
				appLister: mockappLister,
				prompt:    mockprompt,
			}
			tc.setupMocks(mocks)

			sel := Select{
				prompt: mockprompt,
				config: mockappLister,
			}

			got, err := sel.Application("Select an app", "Help text")
			if tc.wantErr != nil {
				require.EqualError(t, tc.wantErr, err.Error())
			} else {
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestWorkspaceSelect_Dockerfile(t *testing.T) {
	dockerfiles := []string{
		"./Dockerfile",
		"backend/Dockerfile",
		"frontend/Dockerfile",
	}
	dockerfileOptions := []string{
		"./Dockerfile",
		"backend/Dockerfile",
		"frontend/Dockerfile",
		"Enter custom path for your Dockerfile",
		"Use an existing image instead",
	}
	testCases := map[string]struct {
		mockWs     func(retriever *mocks.MockWorkspaceRetriever)
		mockPrompt func(*mocks.MockPrompter)

		wantedErr        error
		wantedDockerfile string
	}{
		"choose an existing Dockerfile": {
			mockWs: func(m *mocks.MockWorkspaceRetriever) {
				m.EXPECT().ListDockerfiles().Return(dockerfiles, nil)
			},
			mockPrompt: func(m *mocks.MockPrompter) {
				m.EXPECT().SelectOne(
					gomock.Any(), gomock.Any(),
					gomock.Eq(dockerfileOptions),
					gomock.Any(),
				).Return("frontend/Dockerfile", nil)
			},
			wantedErr:        nil,
			wantedDockerfile: "frontend/Dockerfile",
		},
		"prompts user for custom path": {
			mockWs: func(m *mocks.MockWorkspaceRetriever) {
				m.EXPECT().ListDockerfiles().Return([]string{}, nil)
			},
			mockPrompt: func(m *mocks.MockPrompter) {
				m.EXPECT().SelectOne(
					gomock.Any(), gomock.Any(),
					gomock.Eq([]string{
						"Enter custom path for your Dockerfile",
						"Use an existing image instead",
					}),
					gomock.Any(),
				).Return("Enter custom path for your Dockerfile", nil)
				m.EXPECT().Get(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return("crazy/path/Dockerfile", nil)
			},
			wantedErr:        nil,
			wantedDockerfile: "crazy/path/Dockerfile",
		},
		"returns an error if fail to list Dockerfile": {
			mockWs: func(m *mocks.MockWorkspaceRetriever) {
				m.EXPECT().ListDockerfiles().Return(nil, errors.New("some error"))
			},
			mockPrompt: func(m *mocks.MockPrompter) {},
			wantedErr:  fmt.Errorf("list Dockerfiles: some error"),
		},
		"returns an error if fail to select Dockerfile": {
			mockWs: func(m *mocks.MockWorkspaceRetriever) {
				m.EXPECT().ListDockerfiles().Return(dockerfiles, nil)
			},
			mockPrompt: func(m *mocks.MockPrompter) {
				m.EXPECT().SelectOne(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return("", errors.New("some error"))
			},
			wantedErr: fmt.Errorf("select Dockerfile: some error"),
		},
		"returns an error if fail to get custom Dockerfile path": {
			mockWs: func(m *mocks.MockWorkspaceRetriever) {
				m.EXPECT().ListDockerfiles().Return(dockerfiles, nil)
			},
			mockPrompt: func(m *mocks.MockPrompter) {
				m.EXPECT().SelectOne(
					gomock.Any(), gomock.Any(),
					gomock.Eq(dockerfileOptions),
					gomock.Any(),
				).Return("Enter custom path for your Dockerfile", nil)
				m.EXPECT().Get(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return("", errors.New("some error"))
			},
			wantedErr: fmt.Errorf("get custom Dockerfile path: some error"),
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			p := mocks.NewMockPrompter(ctrl)
			s := mocks.NewMockConfigLister(ctrl)
			cfg := mocks.NewMockWorkspaceRetriever(ctrl)
			tc.mockPrompt(p)
			tc.mockWs(cfg)

			sel := WorkspaceSelect{
				Select: &Select{
					prompt: p,
					config: s,
				},
				ws:      cfg,
				appName: "app-name",
			}

			mockPromptText := "prompt"
			mockHelpText := "help"

			// WHEN
			dockerfile, err := sel.Dockerfile(
				mockPromptText,
				mockPromptText,
				mockHelpText,
				mockHelpText,
				func(v interface{}) error { return nil },
			)

			// THEN
			if tc.wantedErr != nil {
				require.EqualError(t, err, tc.wantedErr.Error())
			} else {
				require.Equal(t, tc.wantedDockerfile, dockerfile)
			}
		})
	}
}

func TestWorkspaceSelect_Schedule(t *testing.T) {
	scheduleTypePrompt := "HAY WHAT SCHEDULE"
	scheduleTypeHelp := "NO"

	testCases := map[string]struct {
		mockWs         func(retriever *mocks.MockWorkspaceRetriever)
		mockPrompt     func(*mocks.MockPrompter)
		wantedSchedule string
		wantedErr      error
	}{
		"error asking schedule type": {
			mockPrompt: func(m *mocks.MockPrompter) {
				gomock.InOrder(
					m.EXPECT().SelectOne(scheduleTypePrompt, scheduleTypeHelp, scheduleTypes, gomock.Any()).Return("", errors.New("some error")),
				)
			},
			wantedErr: errors.New("get schedule type: some error"),
		},
		"ask for rate": {
			mockPrompt: func(m *mocks.MockPrompter) {
				gomock.InOrder(
					m.EXPECT().SelectOne(scheduleTypePrompt, scheduleTypeHelp, scheduleTypes, gomock.Any()).Return(rate, nil),
					m.EXPECT().Get(ratePrompt, rateHelp, gomock.Any(), gomock.Any()).Return("1h30m", nil),
				)
			},
			wantedSchedule: "@every 1h30m",
		},
		"error getting rate": {
			mockPrompt: func(m *mocks.MockPrompter) {
				gomock.InOrder(
					m.EXPECT().SelectOne(scheduleTypePrompt, scheduleTypeHelp, scheduleTypes, gomock.Any()).Return(rate, nil),
					m.EXPECT().Get(ratePrompt, rateHelp, gomock.Any(), gomock.Any()).Return("", fmt.Errorf("some error")),
				)
			},
			wantedErr: errors.New("get schedule rate: some error"),
		},
		"ask for cron": {
			mockPrompt: func(m *mocks.MockPrompter) {
				gomock.InOrder(
					m.EXPECT().SelectOne(scheduleTypePrompt, scheduleTypeHelp, scheduleTypes, gomock.Any()).Return(fixedSchedule, nil),
					m.EXPECT().SelectOne(schedulePrompt, scheduleHelp, presetSchedules, gomock.Any()).Return("Daily", nil),
				)
			},
			wantedSchedule: "@daily",
		},
		"error getting cron": {
			mockPrompt: func(m *mocks.MockPrompter) {
				gomock.InOrder(
					m.EXPECT().SelectOne(scheduleTypePrompt, scheduleTypeHelp, scheduleTypes, gomock.Any()).Return(fixedSchedule, nil),
					m.EXPECT().SelectOne(schedulePrompt, scheduleHelp, presetSchedules, gomock.Any()).Return("", errors.New("some error")),
				)
			},
			wantedErr: errors.New("get preset schedule: some error"),
		},
		"ask for custom schedule": {
			mockPrompt: func(m *mocks.MockPrompter) {
				gomock.InOrder(
					m.EXPECT().SelectOne(scheduleTypePrompt, scheduleTypeHelp, scheduleTypes, gomock.Any()).Return(fixedSchedule, nil),
					m.EXPECT().SelectOne(schedulePrompt, scheduleHelp, presetSchedules, gomock.Any()).Return("Custom", nil),
					m.EXPECT().Get(customSchedulePrompt, customScheduleHelp, gomock.Any(), gomock.Any()).Return("0 * * * *", nil),
					m.EXPECT().Confirm(humanReadableCronConfirmPrompt, humanReadableCronConfirmHelp).Return(true, nil),
				)
			},
			wantedSchedule: "0 * * * *",
		},
		"error getting custom schedule": {
			mockPrompt: func(m *mocks.MockPrompter) {
				gomock.InOrder(
					m.EXPECT().SelectOne(scheduleTypePrompt, scheduleTypeHelp, scheduleTypes, gomock.Any()).Return(fixedSchedule, nil),
					m.EXPECT().SelectOne(schedulePrompt, scheduleHelp, presetSchedules, gomock.Any()).Return("Custom", nil),
					m.EXPECT().Get(customSchedulePrompt, customScheduleHelp, gomock.Any(), gomock.Any()).Return("", errors.New("some error")),
				)
			},
			wantedErr: errors.New("get custom schedule: some error"),
		},
		"error confirming custom schedule": {
			mockPrompt: func(m *mocks.MockPrompter) {
				gomock.InOrder(
					m.EXPECT().SelectOne(scheduleTypePrompt, scheduleTypeHelp, scheduleTypes, gomock.Any()).Return(fixedSchedule, nil),
					m.EXPECT().SelectOne(schedulePrompt, scheduleHelp, presetSchedules, gomock.Any()).Return("Custom", nil),
					m.EXPECT().Get(customSchedulePrompt, customScheduleHelp, gomock.Any(), gomock.Any()).Return("0 * * * *", nil),
					m.EXPECT().Confirm(humanReadableCronConfirmPrompt, humanReadableCronConfirmHelp).Return(false, errors.New("some error")),
				)
			},
			wantedErr: errors.New("confirm cron schedule: some error"),
		},
		"custom schedule using valid definition string results in no confirm": {
			mockPrompt: func(m *mocks.MockPrompter) {
				gomock.InOrder(
					m.EXPECT().SelectOne(scheduleTypePrompt, scheduleTypeHelp, scheduleTypes, gomock.Any()).Return(fixedSchedule, nil),
					m.EXPECT().SelectOne(schedulePrompt, scheduleHelp, presetSchedules, gomock.Any()).Return("Custom", nil),
					m.EXPECT().Get(customSchedulePrompt, customScheduleHelp, gomock.Any(), gomock.Any()).Return("@hourly", nil),
				)
			},
			wantedSchedule: "@hourly",
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			p := mocks.NewMockPrompter(ctrl)
			s := mocks.NewMockConfigLister(ctrl)
			cfg := mocks.NewMockWorkspaceRetriever(ctrl)
			tc.mockPrompt(p)
			sel := WorkspaceSelect{
				Select: &Select{
					prompt: p,
					config: s,
				},
				ws:      cfg,
				appName: "app-name",
			}

			var mockValidator prompt.ValidatorFunc = func(interface{}) error { return nil }

			// WHEN
			schedule, err := sel.Schedule(scheduleTypePrompt, scheduleTypeHelp, mockValidator, mockValidator)

			// THEN
			if tc.wantedErr != nil {
				require.EqualError(t, err, tc.wantedErr.Error())
			} else {
				require.Equal(t, tc.wantedSchedule, schedule)
			}
		})
	}
}
