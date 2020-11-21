// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/copilot-cli/internal/pkg/template"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestUnmarshalSvc(t *testing.T) {
	testCases := map[string]struct {
		inContent string

		requireCorrectValues func(t *testing.T, i interface{})
		wantedErr            error
	}{
		"load balanced web service": {
			inContent: `
version: 1.0
name: frontend
type: "Load Balanced Web Service"
image:
  location: foo/bar
  port: 80
cpu: 512
memory: 1024
count: 1
http:
  path: "svc"
  target_container: "frontend"
variables:
  LOG_LEVEL: "WARN"
secrets:
  DB_PASSWORD: MYSQL_DB_PASSWORD
sidecars:
  xray:
    port: 2000/udp
    image: 123456789012.dkr.ecr.us-east-2.amazonaws.com/xray-daemon
    credentialsParameter: some arn
logging:
  destination:
    Name: cloudwatch
    include-pattern: ^[a-z][aeiou].*$
    exclude-pattern: ^.*[aeiou]$
  enableMetadata: false
  secretOptions:
    LOG_TOKEN: LOG_TOKEN
  configFilePath: /extra.conf
environments:
  test:
    count: 3
  prod:
    count:
      range: 1-10
      cpu_percentage: 70
`,
			requireCorrectValues: func(t *testing.T, i interface{}) {
				mockRange := Range("1-10")
				actualManifest, ok := i.(*LoadBalancedWebService)
				require.True(t, ok)
				wantedManifest := &LoadBalancedWebService{
					Workload: Workload{Name: aws.String("frontend"), Type: aws.String(LoadBalancedWebServiceType)},
					LoadBalancedWebServiceConfig: LoadBalancedWebServiceConfig{
						ImageConfig: ServiceImageWithPort{Image: Image{Build: BuildArgsOrString{},
							Location: aws.String("foo/bar"),
						}, Port: aws.Uint16(80)},
						RoutingRule: RoutingRule{
							Path:            aws.String("svc"),
							TargetContainer: aws.String("frontend"),
							HealthCheck: HealthCheckArgsOrString{
								HealthCheckPath: aws.String("/"),
							},
						},
						TaskConfig: TaskConfig{
							CPU:    aws.Int(512),
							Memory: aws.Int(1024),
							Count: Count{
								Value: aws.Int(1),
							},
							Variables: map[string]string{
								"LOG_LEVEL": "WARN",
							},
							Secrets: map[string]string{
								"DB_PASSWORD": "MYSQL_DB_PASSWORD",
							},
						},
						Sidecar: Sidecar{
							Sidecars: map[string]*SidecarConfig{
								"xray": {
									Port:       aws.String("2000/udp"),
									Image:      aws.String("123456789012.dkr.ecr.us-east-2.amazonaws.com/xray-daemon"),
									CredsParam: aws.String("some arn"),
								},
							},
						},
						Logging: &Logging{
							Destination: map[string]string{
								"exclude-pattern": "^.*[aeiou]$",
								"include-pattern": "^[a-z][aeiou].*$",
								"Name":            "cloudwatch",
							},
							EnableMetadata: aws.Bool(false),
							ConfigFile:     aws.String("/extra.conf"),
							SecretOptions: map[string]string{
								"LOG_TOKEN": "LOG_TOKEN",
							},
						},
					},
					Environments: map[string]*LoadBalancedWebServiceConfig{
						"test": {
							TaskConfig: TaskConfig{
								Count: Count{
									Value: aws.Int(3),
								},
							},
						},
						"prod": {
							TaskConfig: TaskConfig{
								Count: Count{
									Autoscaling: Autoscaling{
										Range: &mockRange,
										CPU:   aws.Int(70),
									},
								},
							},
						},
					},
				}
				require.Equal(t, wantedManifest, actualManifest)
			},
		},
		"Backend Service": {
			inContent: `
name: subscribers
type: Backend Service
image:
  build: ./subscribers/Dockerfile
  port: 8080
  healthcheck:
    command: ['CMD-SHELL', 'curl http://localhost:5000/ || exit 1']
cpu: 1024
memory: 1024
secrets:
  API_TOKEN: SUBS_API_TOKEN`,
			requireCorrectValues: func(t *testing.T, i interface{}) {
				actualManifest, ok := i.(*BackendService)
				require.True(t, ok)
				wantedManifest := &BackendService{
					Workload: Workload{
						Name: aws.String("subscribers"),
						Type: aws.String(BackendServiceType),
					},
					BackendServiceConfig: BackendServiceConfig{
						ImageConfig: imageWithPortAndHealthcheck{
							ServiceImageWithPort: ServiceImageWithPort{
								Image: Image{
									Build: BuildArgsOrString{
										BuildString: aws.String("./subscribers/Dockerfile"),
									},
								},
								Port: aws.Uint16(8080),
							},
							HealthCheck: &ContainerHealthCheck{
								Command:     []string{"CMD-SHELL", "curl http://localhost:5000/ || exit 1"},
								Interval:    durationp(10 * time.Second),
								Retries:     aws.Int(2),
								Timeout:     durationp(5 * time.Second),
								StartPeriod: durationp(0 * time.Second),
							},
						},
						TaskConfig: TaskConfig{
							CPU:    aws.Int(1024),
							Memory: aws.Int(1024),
							Count: Count{
								Value: aws.Int(1),
							},
							Secrets: map[string]string{
								"API_TOKEN": "SUBS_API_TOKEN",
							},
						},
					},
				}
				require.Equal(t, wantedManifest, actualManifest)
			},
		},
		"invalid svc type": {
			inContent: `
name: CowSvc
type: 'OH NO'
`,
			wantedErr: &ErrInvalidWorkloadType{Type: "OH NO"},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			m, err := UnmarshalWorkload([]byte(tc.inContent))

			if tc.wantedErr != nil {
				require.EqualError(t, err, tc.wantedErr.Error())
			} else {
				tc.requireCorrectValues(t, m)
			}
		})
	}
}

func TestCount_UnmarshalYAML(t *testing.T) {
	mockResponseTime := 500 * time.Millisecond
	mockRange := Range("1-10")
	testCases := map[string]struct {
		inContent []byte

		wantedStruct Count
		wantedError  error
	}{
		"legacy case: simple task count": {
			inContent: []byte(`count: 1`),

			wantedStruct: Count{
				Value: aws.Int(1),
			},
		},
		"With auto scaling enabled": {
			inContent: []byte(`count:
  range: 1-10
  cpu_percentage: 70
  memory_percentage: 80
  requests: 1000
  response_time: 500ms
`),
			wantedStruct: Count{
				Autoscaling: Autoscaling{
					Range:        &mockRange,
					CPU:          aws.Int(70),
					Memory:       aws.Int(80),
					Requests:     aws.Int(1000),
					ResponseTime: &mockResponseTime,
				},
			},
		},
		"Error if unmarshalable": {
			inContent: []byte(`count: badNumber
`),
			wantedError: errUnmarshalCountOpts,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var b TaskConfig
			err := yaml.Unmarshal(tc.inContent, &b)
			if tc.wantedError != nil {
				require.EqualError(t, err, tc.wantedError.Error())
			} else {
				require.NoError(t, err)
				// check memberwise dereferenced pointer equality
				require.Equal(t, tc.wantedStruct.Value, b.Count.Value)
				require.Equal(t, tc.wantedStruct.Autoscaling.Range, b.Count.Autoscaling.Range)
				require.Equal(t, tc.wantedStruct.Autoscaling.CPU, b.Count.Autoscaling.CPU)
				require.Equal(t, tc.wantedStruct.Autoscaling.Memory, b.Count.Autoscaling.Memory)
				require.Equal(t, tc.wantedStruct.Autoscaling.Requests, b.Count.Autoscaling.Requests)
				require.Equal(t, tc.wantedStruct.Autoscaling.ResponseTime, b.Count.Autoscaling.ResponseTime)
			}
		})
	}
}

func TestRange_Parse(t *testing.T) {
	testCases := map[string]struct {
		inRange string

		wantedMin int
		wantedMax int
		wantedErr error
	}{
		"invalid format": {
			inRange: "badRange",

			wantedErr: fmt.Errorf("invalid range value badRange. Should be in format of ${min}-${max}"),
		},
		"invalid minimum": {
			inRange: "a-100",

			wantedErr: fmt.Errorf("cannot convert minimum value a to integer"),
		},
		"invalid maximum": {
			inRange: "1-a",

			wantedErr: fmt.Errorf("cannot convert maximum value a to integer"),
		},
		"success": {
			inRange: "1-10",

			wantedMin: 1,
			wantedMax: 10,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			r := Range(tc.inRange)
			gotMin, gotMax, err := r.Parse()

			if tc.wantedErr != nil {
				require.EqualError(t, err, tc.wantedErr.Error())
			} else {
				require.NoError(t, err)
				require.Equal(t, gotMin, tc.wantedMin)
				require.Equal(t, gotMax, tc.wantedMax)
			}
		})
	}
}

func TestAutoscaling_Options(t *testing.T) {
	const (
		mockRange    = "1-100"
		mockRequests = 1000
	)
	mockResponseTime := 512 * time.Millisecond
	testCases := map[string]struct {
		inRange        Range
		inCPU          int
		inMemory       int
		inRequests     int
		inResponseTime time.Duration

		wanted    *template.AutoscalingOpts
		wantedErr error
	}{
		"invalid range": {
			inRange: "badRange",

			wantedErr: fmt.Errorf("invalid range value badRange. Should be in format of ${min}-${max}"),
		},
		"success": {
			inRange:        mockRange,
			inCPU:          70,
			inMemory:       80,
			inRequests:     mockRequests,
			inResponseTime: mockResponseTime,

			wanted: &template.AutoscalingOpts{
				MaxCapacity:  aws.Int(100),
				MinCapacity:  aws.Int(1),
				CPU:          aws.Float64(70),
				Memory:       aws.Float64(80),
				Requests:     aws.Float64(1000),
				ResponseTime: aws.Float64(0.512),
			},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			a := Autoscaling{
				Range:        &tc.inRange,
				CPU:          aws.Int(tc.inCPU),
				Memory:       aws.Int(tc.inMemory),
				Requests:     aws.Int(tc.inRequests),
				ResponseTime: &tc.inResponseTime,
			}
			got, err := a.Options()

			if tc.wantedErr != nil {
				require.EqualError(t, err, tc.wantedErr.Error())
			} else {
				require.NoError(t, err)
				require.Equal(t, got, tc.wanted)
			}
		})
	}
}

func Test_ServiceDockerfileBuildRequired(t *testing.T) {
	testCases := map[string]struct {
		svc interface{}

		wanted    bool
		wantedErr error
	}{
		"invalid type": {
			svc: struct{}{},

			wantedErr: fmt.Errorf("service does not have required methods BuildRequired()"),
		},
		"fail to check": {
			svc: &LoadBalancedWebService{},

			wantedErr: fmt.Errorf("check if service requires building from local Dockerfile: either \"image.build\" or \"image.location\" needs to be specified in the manifest"),
		},
		"success with false": {
			svc: &LoadBalancedWebService{
				LoadBalancedWebServiceConfig: LoadBalancedWebServiceConfig{
					ImageConfig: ServiceImageWithPort{
						Image: Image{
							Location: aws.String("mockLocation"),
						},
					},
				},
			},
		},
		"success with true": {
			svc: &LoadBalancedWebService{
				LoadBalancedWebServiceConfig: LoadBalancedWebServiceConfig{
					ImageConfig: ServiceImageWithPort{
						Image: Image{
							Build: BuildArgsOrString{
								BuildString: aws.String("mockDockerfile"),
							},
						},
					},
				},
			},
			wanted: true,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {

			got, err := ServiceDockerfileBuildRequired(tc.svc)

			if tc.wantedErr != nil {
				require.EqualError(t, err, tc.wantedErr.Error())
			} else {
				require.NoError(t, err)
				require.Equal(t, got, tc.wanted)
			}
		})
	}
}
