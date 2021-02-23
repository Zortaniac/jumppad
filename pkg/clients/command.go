package clients

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/shipyard-run/gohup"
)

var ErrorCommandTimeout = fmt.Errorf("Command timed out before completing")

type CommandConfig struct {
	Command          string
	Args             []string
	Env              []string
	WorkingDirectory string
	RunInBackground  bool
	LogFilePath      string
}

type Command interface {
	Execute(config CommandConfig) (int, error)
	Kill(pid int) error
}

// Command executes local commands
type CommandImpl struct {
	timeout time.Duration
	log     hclog.Logger
}

// NewCommand creates a new command with the given logger and maximum command time
func NewCommand(maxCommandTime time.Duration, l hclog.Logger) Command {
	return &CommandImpl{maxCommandTime, l}
}

type done struct {
	pid int
	err error
}

// Execute the given command
func (c *CommandImpl) Execute(config CommandConfig) (int, error) {
	lp := &gohup.LocalProcess{}
	o := gohup.Options{
		Path:    config.Command,
		Args:    config.Args,
		Logfile: config.LogFilePath,
	}

	// add the default environment variables
	o.Env = os.Environ()

	if config.Env != nil {
		o.Env = append(o.Env, config.Args...)
	}

	if config.WorkingDirectory != "" {
		o.Dir = config.WorkingDirectory
	}

	// done chan
	doneCh := make(chan done)

	// wait for timeout
	t := time.After(c.timeout)
	var pidfile string
	var pid int
	var err error

	go func() {
		c.log.Debug(
			"Running command",
			"cmd", config.Command,
			"args", config.Args,
			"dir", config.WorkingDirectory,
			"env", config.Env,
			"pid", pidfile,
			"background", config.RunInBackground,
			"log_file", config.LogFilePath,
		)

		pid, pidfile, err = lp.Start(o)
		if err != nil {
			doneCh <- done{err: err}
		}

		// if not background wait for complete
		if !config.RunInBackground {
			for {
				s, err := lp.QueryStatus(pidfile)
				if err != nil {
					doneCh <- done{err: err, pid: pid}
				}

				if s == gohup.StatusStopped {
					break
				}

				time.Sleep(200 * time.Millisecond)
			}
		}

		doneCh <- done{err: err, pid: pid}
	}()

	select {
	case <-t:
		// kill the running process
		lp.Stop(pidfile)
		return pid, ErrorCommandTimeout
	case d := <-doneCh:
		return d.pid, d.err
	}
}

// Kill a process with the given pid
func (c *CommandImpl) Kill(pid int) error {
	lp := gohup.LocalProcess{}

	return lp.Stop(filepath.Join(os.TempDir(), fmt.Sprintf("%d.pid", pid)))
}
