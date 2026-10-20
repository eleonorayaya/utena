package coder

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

type Command struct {
	Name  string
	Args  []string
	Stdin []byte
}

func (c Command) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

type Process interface {
	Wait() error
	Kill() error
}

type Runner interface {
	Run(ctx context.Context, cmd Command) ([]byte, error)
	Start(cmd Command) (Process, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, c Command) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	if c.Stdin != nil {
		cmd.Stdin = bytes.NewReader(c.Stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s %s: %w", c.Name, firstArg(c.Args), ctx.Err())
		}
		return nil, fmt.Errorf("%s %s: %s: %w", c.Name, firstArg(c.Args), strings.TrimSpace(stderr.String()), err)
	}
	return stdout.Bytes(), nil
}

func (ExecRunner) Start(c Command) (Process, error) {
	cmd := exec.Command(c.Name, c.Args...)
	stderr := &tailBuffer{max: 4096}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return execProcess{cmd: cmd, stderr: stderr}, nil
}

type execProcess struct {
	cmd    *exec.Cmd
	stderr *tailBuffer
}

func (p execProcess) Wait() error {
	err := p.cmd.Wait()
	if err != nil {
		if msg := strings.TrimSpace(p.stderr.String()); msg != "" {
			return fmt.Errorf("%s: %w", msg, err)
		}
	}
	return err
}

type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = b.buf[len(b.buf)-b.max:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

func (p execProcess) Kill() error {
	return p.cmd.Process.Kill()
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
