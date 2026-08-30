package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const promptPlaceholder = "{{prompt}}"

type Job struct {
	Agent   Agent
	Dir     string
	Prompt  string
	Token   string
	Timeout time.Duration
}

func Run(ctx context.Context, job Job) (string, error) {
	if !job.Agent.Registered() {
		return "", fmt.Errorf("%s is reserved but not registered", job.Agent.Name)
	}

	ctx, cancel := context.WithTimeout(ctx, job.Timeout)
	defer cancel()

	result := filepath.Join(job.Dir, ResultFile)
	if err := os.Remove(result); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	argv := make([]string, len(job.Agent.Argv))
	for i, arg := range job.Agent.Argv {
		argv[i] = strings.ReplaceAll(arg, promptPlaceholder, job.Prompt)
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = job.Dir
	cmd.Env = append(os.Environ(), "ELEMENT_AGENT_TOKEN="+job.Token)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("timed out after %s", job.Timeout)
		}
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return "", fmt.Errorf("%s: %w", detail, err)
		}
		return "", err
	}

	written, err := os.ReadFile(result)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if answer := strings.TrimSpace(string(written)); answer != "" {
		return answer, nil
	}
	return strings.TrimSpace(stdout.String()), nil
}
