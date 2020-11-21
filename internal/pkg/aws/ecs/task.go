// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Package ecs provides a client to make API requests to Amazon Elastic Container Service.
package ecs

import (
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/copilot-cli/internal/pkg/term/color"
	"github.com/dustin/go-humanize"
)

const (
	shortTaskIDLength      = 8
	shortImageDigestLength = 8
	imageDigestPrefix      = "sha256:"
)

// humanizeTime is overriden in tests so that its output is constant as time passes.
var humanizeTime = humanize.Time

// Image contains very basic info of a container image.
type Image struct {
	ID     string
	Digest string
}

// Task wraps up ECS Task struct.
type Task ecs.Task

// TaskStatus returns the status of the running task.
func (t *Task) TaskStatus() (*TaskStatus, error) {
	taskID, err := TaskID(aws.StringValue(t.TaskArn))
	if err != nil {
		return nil, err
	}
	var startedAt, stoppedAt time.Time
	var stoppedReason string

	if t.StoppedAt != nil {
		stoppedAt = *t.StoppedAt
	}
	if t.StartedAt != nil {
		startedAt = *t.StartedAt
	}
	if t.StoppedReason != nil {
		stoppedReason = aws.StringValue(t.StoppedReason)
	}
	var images []Image
	for _, container := range t.Containers {
		images = append(images, Image{
			ID:     aws.StringValue(container.Image),
			Digest: imageDigestValue(aws.StringValue(container.ImageDigest)),
		})
	}
	return &TaskStatus{
		Health:        aws.StringValue(t.HealthStatus),
		ID:            taskID,
		Images:        images,
		LastStatus:    aws.StringValue(t.LastStatus),
		StartedAt:     startedAt,
		StoppedAt:     stoppedAt,
		StoppedReason: stoppedReason,
	}, nil
}

// TaskStatus contains the status info of a task.
type TaskStatus struct {
	Health        string    `json:"health"`
	ID            string    `json:"id"`
	Images        []Image   `json:"images"`
	LastStatus    string    `json:"lastStatus"`
	StartedAt     time.Time `json:"startedAt"`
	StoppedAt     time.Time `json:"stoppedAt"`
	StoppedReason string    `json:"stoppedReason"`
}

// HumanString returns the stringified TaskStatus struct with human readable format.
// Example output:
//   6ca7a60d          f884127d            RUNNING             UNKNOWN             19 hours ago        -
func (t TaskStatus) HumanString() string {
	var digest []string
	imageDigest := "-"
	for _, image := range t.Images {
		if len(image.Digest) < shortImageDigestLength {
			continue
		}
		digest = append(digest, image.Digest[:shortImageDigestLength])
	}
	if len(digest) != 0 {
		imageDigest = strings.Join(digest, ",")
	}
	startedSince := "-"
	if !t.StartedAt.IsZero() {
		startedSince = humanizeTime(t.StartedAt)
	}
	stoppedSince := "-"
	if !t.StoppedAt.IsZero() {
		stoppedSince = humanizeTime(t.StoppedAt)
	}
	shortTaskID := "-"
	if len(t.ID) >= shortTaskIDLength {
		shortTaskID = t.ID[:shortTaskIDLength]
	}
	return fmt.Sprintf("  %s\t%s\t%s\t%s\t%s\t%s\n", shortTaskID, imageDigest, t.LastStatus, startedSince, stoppedSince, taskHealthColor(t.Health))
}

// TaskDefinition wraps up ECS TaskDefinition struct.
type TaskDefinition ecs.TaskDefinition

// EnvironmentVariables returns environment variables of the task definition.
func (t *TaskDefinition) EnvironmentVariables() map[string]string {
	envs := make(map[string]string)
	for _, env := range t.ContainerDefinitions[0].Environment {
		envs[aws.StringValue(env.Name)] = aws.StringValue(env.Value)
	}
	return envs
}

// TaskID parses the task ARN and returns the task ID.
// For example: arn:aws:ecs:us-west-2:123456789:task/my-project-test-Cluster-9F7Y0RLP60R7/4082490ee6c245e09d2145010aa1ba8d,
// arn:aws:ecs:us-west-2:123456789:task/4082490ee6c245e09d2145010aa1ba8d
// return 4082490ee6c245e09d2145010aa1ba8d.
func TaskID(taskARN string) (string, error) {
	parsedARN, err := arn.Parse(taskARN)
	if err != nil {
		return "", fmt.Errorf("parse ECS task ARN: %w", err)
	}
	resources := strings.Split(parsedARN.Resource, "/")
	taskID := resources[len(resources)-1]
	return taskID, nil
}

func taskHealthColor(status string) string {
	switch status {
	case "HEALTHY":
		return color.Green.Sprint(status)
	case "UNHEALTHY":
		return color.Red.Sprint(status)
	case "UNKNOWN":
		return color.Yellow.Sprint(status)
	default:
		return status
	}
}

// imageDigestValue strips the hash function prefix, such as "sha256:", from the digest.
// For example: sha256:18f7eb6cff6e63e5f5273fb53f672975fe6044580f66c354f55d2de8dd28aec7
// becomes 18f7eb6cff6e63e5f5273fb53f672975fe6044580f66c354f55d2de8dd28aec7.
func imageDigestValue(digest string) string {
	return strings.TrimPrefix(digest, imageDigestPrefix)
}
