// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Package ecs provides a client to retrieve Copilot ECS information.
package ecs

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/copilot-cli/internal/pkg/aws/ecs"
	"github.com/aws/copilot-cli/internal/pkg/aws/resourcegroups"
	"github.com/aws/copilot-cli/internal/pkg/deploy"
)

const (
	fmtWorkloadTaskDefinitionFamily = "%s-%s-%s"
	clusterResourceType             = "ecs:cluster"
)

type resourceGetter interface {
	GetResourcesByTags(resourceType string, tags map[string]string) ([]*resourcegroups.Resource, error)
}

type runningTasksInFamilyGetter interface {
	RunningTasksInFamily(cluster, family string) ([]*ecs.Task, error)
}

// Client retrieves Copilot information from ECS endpoint.
type Client struct {
	rgGetter   resourceGetter
	taskGetter runningTasksInFamilyGetter
}

// New inits a new Client.
func New(sess *session.Session) *Client {
	return &Client{
		rgGetter:   resourcegroups.New(sess),
		taskGetter: ecs.New(sess),
	}
}

// Cluster returns the ARN of the cluster in an environment.
func (c Client) Cluster(app, env string) (string, error) {
	clusters, err := c.rgGetter.GetResourcesByTags(clusterResourceType, map[string]string{
		deploy.AppTagKey: app,
		deploy.EnvTagKey: env,
	})

	if err != nil {
		return "", fmt.Errorf("get cluster resources for environment %s: %w", env, err)
	}

	if len(clusters) == 0 {
		return "", fmt.Errorf("no cluster found in environment %s", env)
	}

	// NOTE: only one cluster is associated with an application and an environment.
	if len(clusters) > 1 {
		return "", fmt.Errorf("more than one cluster is found in environment %s", env)
	}
	return clusters[0].ARN, nil
}

// ListActiveWorkloadTasks lists all active workload tasks (with desired status to be RUNNING) in the environment.
func (c Client) ListActiveWorkloadTasks(app, env, workload string) (clusterARN string, taskARNs []string, err error) {
	clusterARN, err = c.Cluster(app, env)
	if err != nil {
		return "", nil, fmt.Errorf("get cluster for env %s: %w", env, err)
	}
	tdFamilyName := fmt.Sprintf(fmtWorkloadTaskDefinitionFamily, app, env, workload)
	tasks, err := c.taskGetter.RunningTasksInFamily(clusterARN, tdFamilyName)
	if err != nil {
		return "", nil, fmt.Errorf("list tasks that belong to family %s: %w", tdFamilyName, err)
	}
	for _, task := range tasks {
		taskARNs = append(taskARNs, *task.TaskArn)
	}
	return
}
