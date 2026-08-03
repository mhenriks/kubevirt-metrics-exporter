package cri

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	runtimev1 "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type ContainerInfo struct {
	ContainerID string
	PID         int
}

type Client struct {
	conn *grpc.ClientConn
	rc   runtimev1.RuntimeServiceClient
}

func NewClient(socketPath string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, "unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("connecting to CRI socket %s: %w", socketPath, err)
	}

	return &Client{
		conn: conn,
		rc:   runtimev1.NewRuntimeServiceClient(conn),
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) FindComputePID(ctx context.Context, podName, namespace string) (*ContainerInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := c.rc.ListContainers(ctx, &runtimev1.ListContainersRequest{
		Filter: &runtimev1.ContainerFilter{
			State: &runtimev1.ContainerStateValue{
				State: runtimev1.ContainerState_CONTAINER_RUNNING,
			},
			LabelSelector: map[string]string{
				"io.kubernetes.container.name": "compute",
				"io.kubernetes.pod.name":       podName,
				"io.kubernetes.pod.namespace":  namespace,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("listing containers for %s/%s: %w", namespace, podName, err)
	}

	if len(resp.Containers) == 0 {
		return nil, fmt.Errorf("no running compute container found for %s/%s", namespace, podName)
	}

	container := resp.Containers[0]
	pid, err := c.extractPID(ctx, container.Id)
	if err != nil {
		return nil, err
	}

	return &ContainerInfo{
		ContainerID: container.Id,
		PID:         pid,
	}, nil
}

func (c *Client) extractPID(ctx context.Context, containerID string) (int, error) {
	resp, err := c.rc.ContainerStatus(ctx, &runtimev1.ContainerStatusRequest{
		ContainerId: containerID,
		Verbose:     true,
	})
	if err != nil {
		return 0, fmt.Errorf("getting container status for %s: %w", containerID, err)
	}

	return parsePID(resp.Info)
}

func parsePID(info map[string]string) (int, error) {
	infoJSON, ok := info["info"]
	if !ok {
		return 0, fmt.Errorf("no 'info' key in container status verbose response")
	}

	var parsed struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal([]byte(infoJSON), &parsed); err != nil {
		return 0, fmt.Errorf("parsing info JSON: %w", err)
	}
	if parsed.PID > 0 {
		return parsed.PID, nil
	}

	var nested map[string]json.RawMessage
	if err := json.Unmarshal([]byte(infoJSON), &nested); err == nil {
		if initPID, ok := nested["init_pid"]; ok {
			var pid int
			if json.Unmarshal(initPID, &pid) == nil && pid > 0 {
				return pid, nil
			}
		}
	}

	return 0, fmt.Errorf("could not extract PID from container info")
}
