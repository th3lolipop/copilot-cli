// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Package task provides support for running Amazon ECS tasks.
package task

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/copilot-cli/internal/pkg/aws/ec2"
	"github.com/aws/copilot-cli/internal/pkg/aws/ecs"
)

// VPCGetter wraps methods of getting VPC info.
type VPCGetter interface {
	SubnetIDs(filters ...ec2.Filter) ([]string, error)
	SecurityGroups(filters ...ec2.Filter) ([]string, error)
	PublicSubnetIDs(filters ...ec2.Filter) ([]string, error)
}

// ClusterGetter wraps the method of getting a cluster ARN.
type ClusterGetter interface {
	Cluster(app, env string) (string, error)
}

// DefaultClusterGetter wraps the method of getting a default cluster ARN.
type DefaultClusterGetter interface {
	DefaultCluster() (string, error)
}

// Runner wraps the method of running tasks.
type Runner interface {
	RunTask(input ecs.RunTaskInput) ([]*ecs.Task, error)
}

// Task represents a one-off workload that runs until completed or an error occurs.
type Task struct {
	TaskARN    string
	ClusterARN string
	StartedAt  *time.Time
}

const (
	startedBy = "copilot-task"
)

var (
	fmtTaskFamilyName = "copilot-%s"
)

func taskFamilyName(groupName string) string {
	return fmt.Sprintf(fmtTaskFamilyName, groupName)
}

func newTaskFromECS(ecsTask *ecs.Task) *Task {
	return &Task{
		TaskARN:    aws.StringValue(ecsTask.TaskArn),
		ClusterARN: aws.StringValue(ecsTask.ClusterArn),
		StartedAt:  ecsTask.StartedAt,
	}
}

func convertECSTasks(ecsTasks []*ecs.Task) []*Task {
	tasks := make([]*Task, len(ecsTasks))
	for idx, ecsTask := range ecsTasks {
		tasks[idx] = newTaskFromECS(ecsTask)
	}
	return tasks
}
