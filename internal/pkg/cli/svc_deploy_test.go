// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	addon "github.com/aws/copilot-cli/internal/pkg/addon"
	"github.com/aws/copilot-cli/internal/pkg/config"
	"github.com/aws/copilot-cli/internal/pkg/deploy/cloudformation/stack"
	"github.com/aws/copilot-cli/internal/pkg/docker"
	"github.com/aws/copilot-cli/internal/pkg/manifest"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/aws/copilot-cli/internal/pkg/cli/mocks"
)

type deploySvcMocks struct {
	mockWs                 *mocks.MockwsSvcDirReader
	mockimageBuilderPusher *mocks.MockimageBuilderPusher
}

func TestSvcDeployOpts_Validate(t *testing.T) {
	testCases := map[string]struct {
		inAppName string
		inEnvName string
		inSvcName string

		mockWs    func(m *mocks.MockwsSvcDirReader)
		mockStore func(m *mocks.Mockstore)

		wantedError error
	}{
		"no existing applications": {
			mockWs:    func(m *mocks.MockwsSvcDirReader) {},
			mockStore: func(m *mocks.Mockstore) {},

			wantedError: errNoAppInWorkspace,
		},
		"with workspace error": {
			inAppName: "phonetool",
			inSvcName: "frontend",
			mockWs: func(m *mocks.MockwsSvcDirReader) {
				m.EXPECT().ServiceNames().Return(nil, errors.New("some error"))
			},
			mockStore: func(m *mocks.Mockstore) {},

			wantedError: errors.New("list services in the workspace: some error"),
		},
		"with service not in workspace": {
			inAppName: "phonetool",
			inSvcName: "frontend",
			mockWs: func(m *mocks.MockwsSvcDirReader) {
				m.EXPECT().ServiceNames().Return([]string{}, nil)
			},
			mockStore: func(m *mocks.Mockstore) {},

			wantedError: errors.New("service frontend not found in the workspace"),
		},
		"with unknown environment": {
			inAppName: "phonetool",
			inEnvName: "test",
			mockWs:    func(m *mocks.MockwsSvcDirReader) {},
			mockStore: func(m *mocks.Mockstore) {
				m.EXPECT().GetEnvironment("phonetool", "test").
					Return(nil, errors.New("unknown env"))
			},

			wantedError: errors.New("get environment test configuration: unknown env"),
		},
		"successful validation": {
			inAppName: "phonetool",
			inSvcName: "frontend",
			inEnvName: "test",
			mockWs: func(m *mocks.MockwsSvcDirReader) {
				m.EXPECT().ServiceNames().Return([]string{"frontend"}, nil)
			},
			mockStore: func(m *mocks.Mockstore) {
				m.EXPECT().GetEnvironment("phonetool", "test").
					Return(&config.Environment{Name: "test"}, nil)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockWs := mocks.NewMockwsSvcDirReader(ctrl)
			mockStore := mocks.NewMockstore(ctrl)
			tc.mockWs(mockWs)
			tc.mockStore(mockStore)
			opts := deploySvcOpts{
				deployWkldVars: deployWkldVars{
					appName: tc.inAppName,
					name:    tc.inSvcName,
					envName: tc.inEnvName,
				},
				ws:    mockWs,
				store: mockStore,
			}

			// WHEN
			err := opts.Validate()

			// THEN
			if tc.wantedError != nil {
				require.EqualError(t, err, tc.wantedError.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSvcDeployOpts_Ask(t *testing.T) {
	testCases := map[string]struct {
		inAppName  string
		inEnvName  string
		inSvcName  string
		inImageTag string

		wantedCalls func(m *mocks.MockwsSelector)

		wantedSvcName  string
		wantedEnvName  string
		wantedImageTag string
		wantedError    error
	}{
		"prompts for environment name and service names": {
			inAppName:  "phonetool",
			inImageTag: "latest",
			wantedCalls: func(m *mocks.MockwsSelector) {
				m.EXPECT().Service("Select a service in your workspace", "").Return("frontend", nil)
				m.EXPECT().Environment("Select an environment", "", "phonetool").Return("prod-iad", nil)
			},

			wantedSvcName:  "frontend",
			wantedEnvName:  "prod-iad",
			wantedImageTag: "latest",
		},
		"don't call selector if flags are provided": {
			inAppName:  "phonetool",
			inEnvName:  "prod-iad",
			inSvcName:  "frontend",
			inImageTag: "latest",
			wantedCalls: func(m *mocks.MockwsSelector) {
				m.EXPECT().Service(gomock.Any(), gomock.Any()).Times(0)
				m.EXPECT().Environment(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			},

			wantedSvcName:  "frontend",
			wantedEnvName:  "prod-iad",
			wantedImageTag: "latest",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			mockSel := mocks.NewMockwsSelector(ctrl)

			tc.wantedCalls(mockSel)
			opts := deploySvcOpts{
				deployWkldVars: deployWkldVars{
					appName:  tc.inAppName,
					name:     tc.inSvcName,
					envName:  tc.inEnvName,
					imageTag: tc.inImageTag,
				},
				sel: mockSel,
			}

			// WHEN
			err := opts.Ask()

			// THEN
			if tc.wantedError == nil {
				require.NoError(t, err)
				require.Equal(t, tc.wantedSvcName, opts.name)
				require.Equal(t, tc.wantedEnvName, opts.envName)
				require.Equal(t, tc.wantedImageTag, opts.imageTag)
			} else {
				require.EqualError(t, err, tc.wantedError.Error())
			}
		})
	}
}

func TestSvcDeployOpts_configureContainerImage(t *testing.T) {
	mockError := errors.New("mockError")
	mockManifest := []byte(`name: serviceA
type: 'Load Balanced Web Service'
image:
  build:
    dockerfile: path/to/Dockerfile
    context: path
`)
	mockMftNoBuild := []byte(`name: serviceA
type: 'Load Balanced Web Service'
image:
  location: foo/bar
`)
	mockMftBuildString := []byte(`name: serviceA
type: 'Load Balanced Web Service'
image:
  build: path/to/Dockerfile
`)
	mockMftNoContext := []byte(`name: serviceA
type: 'Load Balanced Web Service'
image:
  build:
    dockerfile: path/to/Dockerfile`)

	tests := map[string]struct {
		inputSvc   string
		setupMocks func(mocks deploySvcMocks)

		wantErr error
	}{
		"should return error if ws ReadFile returns error": {
			inputSvc: "serviceA",
			setupMocks: func(m deploySvcMocks) {
				gomock.InOrder(
					m.mockWs.EXPECT().ReadServiceManifest("serviceA").Return(nil, mockError),
				)
			},
			wantErr: fmt.Errorf("read service %s manifest file: %w", "serviceA", mockError),
		},
		"should return error if workspace methods fail": {
			inputSvc: "serviceA",
			setupMocks: func(m deploySvcMocks) {
				gomock.InOrder(
					m.mockWs.EXPECT().ReadServiceManifest(gomock.Any()).Return(mockManifest, nil),
					m.mockWs.EXPECT().CopilotDirPath().Return("", mockError),
				)
			},
			wantErr: fmt.Errorf("get copilot directory: %w", mockError),
		},
		"success without building and pushing": {
			inputSvc: "serviceA",
			setupMocks: func(m deploySvcMocks) {
				gomock.InOrder(
					m.mockWs.EXPECT().ReadServiceManifest("serviceA").Return(mockMftNoBuild, nil),
					m.mockWs.EXPECT().CopilotDirPath().Times(0),
					m.mockimageBuilderPusher.EXPECT().BuildAndPush(gomock.Any(), gomock.Any()).Times(0),
				)
			},
		},
		"should return error if fail to build and push": {
			inputSvc: "serviceA",
			setupMocks: func(m deploySvcMocks) {
				gomock.InOrder(
					m.mockWs.EXPECT().ReadServiceManifest("serviceA").Return(mockManifest, nil),
					m.mockWs.EXPECT().CopilotDirPath().Return("/ws/root/copilot", nil),
					m.mockimageBuilderPusher.EXPECT().BuildAndPush(gomock.Any(), gomock.Any()).Return(mockError),
				)
			},
			wantErr: fmt.Errorf("build and push image: mockError"),
		},
		"success": {
			inputSvc: "serviceA",
			setupMocks: func(m deploySvcMocks) {
				gomock.InOrder(
					m.mockWs.EXPECT().ReadServiceManifest("serviceA").Return(mockManifest, nil),
					m.mockWs.EXPECT().CopilotDirPath().Return("/ws/root/copilot", nil),
					m.mockimageBuilderPusher.EXPECT().BuildAndPush(gomock.Any(), &docker.BuildArguments{
						Dockerfile: filepath.Join("/ws", "root", "path", "to", "Dockerfile"),
						Context:    filepath.Join("/ws", "root", "path"),
					}).Return(nil),
				)
			},
		},
		"using simple buildstring (backwards compatible)": {
			inputSvc: "serviceA",
			setupMocks: func(m deploySvcMocks) {
				gomock.InOrder(
					m.mockWs.EXPECT().ReadServiceManifest("serviceA").Return(mockMftBuildString, nil),
					m.mockWs.EXPECT().CopilotDirPath().Return("/ws/root/copilot", nil),
					m.mockimageBuilderPusher.EXPECT().BuildAndPush(gomock.Any(), &docker.BuildArguments{
						Dockerfile: filepath.Join("/ws", "root", "path", "to", "Dockerfile"),
						Context:    filepath.Join("/ws", "root", "path", "to"),
					}).Return(nil),
				)
			},
		},
		"without context field in overrides": {
			inputSvc: "serviceA",
			setupMocks: func(m deploySvcMocks) {
				gomock.InOrder(
					m.mockWs.EXPECT().ReadServiceManifest("serviceA").Return(mockMftNoContext, nil),
					m.mockWs.EXPECT().CopilotDirPath().Return("/ws/root/copilot", nil),
					m.mockimageBuilderPusher.EXPECT().BuildAndPush(gomock.Any(), &docker.BuildArguments{
						Dockerfile: filepath.Join("/ws", "root", "path", "to", "Dockerfile"),
						Context:    filepath.Join("/ws", "root", "path", "to"),
					}).Return(nil),
				)
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockWorkspace := mocks.NewMockwsSvcDirReader(ctrl)
			mockimageBuilderPusher := mocks.NewMockimageBuilderPusher(ctrl)
			mocks := deploySvcMocks{
				mockWs:                 mockWorkspace,
				mockimageBuilderPusher: mockimageBuilderPusher,
			}
			test.setupMocks(mocks)
			opts := deploySvcOpts{
				deployWkldVars: deployWkldVars{
					name: test.inputSvc,
				},
				unmarshal:          manifest.UnmarshalWorkload,
				imageBuilderPusher: mockimageBuilderPusher,
				ws:                 mockWorkspace,
			}

			gotErr := opts.configureContainerImage()

			if test.wantErr != nil {
				require.EqualError(t, gotErr, test.wantErr.Error())
			} else {
				require.Nil(t, gotErr)
			}
		})
	}
}

func TestSvcDeployOpts_pushAddonsTemplateToS3Bucket(t *testing.T) {
	mockError := errors.New("some error")
	tests := map[string]struct {
		inputSvc      string
		inEnvironment *config.Environment
		inApp         *config.Application

		mockAppResourcesGetter func(m *mocks.MockappResourcesGetter)
		mockS3Svc              func(m *mocks.MockartifactUploader)
		mockAddons             func(m *mocks.Mocktemplater)

		wantPath string
		wantErr  error
	}{
		"should push addons template to S3 bucket": {
			inputSvc: "mockSvc",
			inEnvironment: &config.Environment{
				Name:   "mockEnv",
				Region: "us-west-2",
			},
			inApp: &config.Application{
				Name: "mockApp",
			},
			mockAppResourcesGetter: func(m *mocks.MockappResourcesGetter) {
				m.EXPECT().GetAppResourcesByRegion(&config.Application{
					Name: "mockApp",
				}, "us-west-2").Return(&stack.AppRegionalResources{
					S3Bucket: "mockBucket",
				}, nil)
			},
			mockAddons: func(m *mocks.Mocktemplater) {
				m.EXPECT().Template().Return("some data", nil)
			},
			mockS3Svc: func(m *mocks.MockartifactUploader) {
				m.EXPECT().PutArtifact("mockBucket", "mockSvc.addons.stack.yml", gomock.Any()).Return("https://mockS3DomainName/mockPath", nil)
			},

			wantErr:  nil,
			wantPath: "https://mockS3DomainName/mockPath",
		},
		"should return error if fail to get app resources": {
			inputSvc: "mockSvc",
			inEnvironment: &config.Environment{
				Name:   "mockEnv",
				Region: "us-west-2",
			},
			inApp: &config.Application{
				Name: "mockApp",
			},
			mockAppResourcesGetter: func(m *mocks.MockappResourcesGetter) {
				m.EXPECT().GetAppResourcesByRegion(&config.Application{
					Name: "mockApp",
				}, "us-west-2").Return(nil, mockError)
			},
			mockAddons: func(m *mocks.Mocktemplater) {
				m.EXPECT().Template().Return("some data", nil)
			},
			mockS3Svc: func(m *mocks.MockartifactUploader) {},

			wantErr: fmt.Errorf("get app resources: some error"),
		},
		"should return error if fail to upload to S3 bucket": {
			inputSvc: "mockSvc",
			inEnvironment: &config.Environment{
				Name:   "mockEnv",
				Region: "us-west-2",
			},
			inApp: &config.Application{
				Name: "mockApp",
			},

			mockAppResourcesGetter: func(m *mocks.MockappResourcesGetter) {
				m.EXPECT().GetAppResourcesByRegion(&config.Application{
					Name: "mockApp",
				}, "us-west-2").Return(&stack.AppRegionalResources{
					S3Bucket: "mockBucket",
				}, nil)
			},
			mockAddons: func(m *mocks.Mocktemplater) {
				m.EXPECT().Template().Return("some data", nil)
			},
			mockS3Svc: func(m *mocks.MockartifactUploader) {
				m.EXPECT().PutArtifact("mockBucket", "mockSvc.addons.stack.yml", gomock.Any()).Return("", mockError)
			},

			wantErr: fmt.Errorf("put addons artifact to bucket mockBucket: some error"),
		},
		"should return empty url if the service doesn't have any addons": {
			inputSvc: "mockSvc",
			mockAddons: func(m *mocks.Mocktemplater) {
				m.EXPECT().Template().Return("", &addon.ErrAddonsDirNotExist{
					WlName: "mockSvc",
				})
			},
			mockAppResourcesGetter: func(m *mocks.MockappResourcesGetter) {
				m.EXPECT().GetAppResourcesByRegion(gomock.Any(), gomock.Any()).Times(0)
			},
			mockS3Svc: func(m *mocks.MockartifactUploader) {
				m.EXPECT().PutArtifact(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			},
			wantPath: "",
		},
		"should fail if addons cannot be retrieved from workspace": {
			inputSvc: "mockSvc",
			mockAddons: func(m *mocks.Mocktemplater) {
				m.EXPECT().Template().Return("", mockError)
			},
			mockAppResourcesGetter: func(m *mocks.MockappResourcesGetter) {
				m.EXPECT().GetAppResourcesByRegion(gomock.Any(), gomock.Any()).Times(0)
			},
			mockS3Svc: func(m *mocks.MockartifactUploader) {
				m.EXPECT().PutArtifact(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			},
			wantErr: fmt.Errorf("retrieve addons template: %w", mockError),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockProjectSvc := mocks.NewMockstore(ctrl)
			mockProjectResourcesGetter := mocks.NewMockappResourcesGetter(ctrl)
			mockS3Svc := mocks.NewMockartifactUploader(ctrl)
			mockAddons := mocks.NewMocktemplater(ctrl)
			tc.mockAppResourcesGetter(mockProjectResourcesGetter)
			tc.mockS3Svc(mockS3Svc)
			tc.mockAddons(mockAddons)

			opts := deploySvcOpts{
				deployWkldVars: deployWkldVars{
					name: tc.inputSvc,
				},
				store:             mockProjectSvc,
				appCFN:            mockProjectResourcesGetter,
				addons:            mockAddons,
				s3:                mockS3Svc,
				targetEnvironment: tc.inEnvironment,
				targetApp:         tc.inApp,
			}

			gotPath, gotErr := opts.pushAddonsTemplateToS3Bucket()

			if gotErr != nil {
				require.EqualError(t, gotErr, tc.wantErr.Error())
			} else {
				require.Equal(t, tc.wantPath, gotPath)
			}
		})
	}
}
