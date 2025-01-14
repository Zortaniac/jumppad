package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	htypes "github.com/jumppad-labs/hclconfig/types"
	"github.com/jumppad-labs/jumppad/pkg/clients"
	cmdClient "github.com/jumppad-labs/jumppad/pkg/clients/command"
	cmdTypes "github.com/jumppad-labs/jumppad/pkg/clients/command/types"
	contClient "github.com/jumppad-labs/jumppad/pkg/clients/container"
	"github.com/jumppad-labs/jumppad/pkg/clients/container/types"
	contTypes "github.com/jumppad-labs/jumppad/pkg/clients/container/types"
	"github.com/jumppad-labs/jumppad/pkg/clients/logger"
	"github.com/jumppad-labs/jumppad/pkg/utils"
)

// ExecRemote provider allows the execution of arbitrary commands on an existing target or
// can create a new container before running
type Provider struct {
	config    *Exec
	container contClient.ContainerTasks
	command   cmdClient.Command
	log       logger.Logger
}

// Intit creates a new Exec provider
func (p *Provider) Init(cfg htypes.Resource, l logger.Logger) error {
	c, ok := cfg.(*Exec)
	if !ok {
		return fmt.Errorf("unable to initialize provider, resource is not of type Exec")
	}

	cli, err := clients.GenerateClients(l)
	if err != nil {
		return err
	}

	p.config = c
	p.command = cli.Command
	p.container = cli.ContainerTasks
	p.log = l

	return nil
}

func (p *Provider) Create() error {
	p.log.Info("executing script", "ref", p.config.ID, "script", p.config.Script)

	// check if we have a target or image specified
	if p.config.Image != nil || p.config.Target != nil {
		// remote exec
		err := p.createRemoteExec()
		if err != nil {
			return fmt.Errorf("unable to create remote exec: %w", err)
		}
	} else {
		// local exec
		pid, err := p.createLocalExec()
		if err != nil {
			return fmt.Errorf("unable to create local exec: %w", err)
		}

		p.config.PID = pid
	}

	return nil
}

func (p *Provider) Destroy() error {
	// check that we don't we have a target or image specified as
	// remote execs are not daemonized
	if p.config.Daemon && p.config.Image == nil && p.config.Target == nil {
		if p.config.PID < 1 {
			p.log.Warn("unable to stop local process, no pid")
			return nil
		}

		err := p.command.Kill(p.config.PID)
		if err != nil {
			p.log.Warn("error cleaning up daemonized process", "error", err)
		}
	}

	return nil
}

func (p *Provider) Lookup() ([]string, error) {
	return []string{}, nil
}

func (p *Provider) Refresh() error {
	p.log.Debug("refresh Exec", "ref", p.config.Name)

	return nil
}

func (p *Provider) Changed() (bool, error) {
	p.log.Debug("checking changes", "ref", p.config.ID)

	return false, nil
}

func (p *Provider) createRemoteExec() error {
	// execution target id
	targetID := ""
	if p.config.Target == nil {
		// Not using existing target create new container
		id, err := p.createRemoteExecContainer()
		if err != nil {
			return fmt.Errorf("unable to create container for exec.%s: %w", p.config.Name, err)
		}

		targetID = id
	} else {
		ids, err := p.container.FindContainerIDs(p.config.Target.ContainerName)
		if err != nil {
			return fmt.Errorf("unable to find exec target: %w", err)
		}

		if len(ids) != 1 {
			return fmt.Errorf("unable to find exec target %s", p.config.Target.ContainerName)
		}

		targetID = ids[0]
	}

	// execute the script in the container
	script := p.config.Script

	// build the environment variables
	envs := []string{}

	for k, v := range p.config.Environment {
		envs = append(envs, fmt.Sprintf("%s=%s", k, v))
	}

	user := ""
	group := ""

	if p.config.RunAs != nil {
		user = p.config.RunAs.User
		group = p.config.RunAs.Group
	}

	_, err := p.container.ExecuteScript(targetID, script, envs, p.config.WorkingDirectory, user, group, 300, p.log.StandardWriter())
	if err != nil {
		p.log.Error("error executing command", "ref", p.config.Name, "image", p.config.Image, "script", p.config.Script)
		return fmt.Errorf("unable to execute command: in remote container: %w", err)

	}

	// destroy the container if we created one
	if p.config.Target == nil {
		p.container.RemoveContainer(targetID, true)
	}

	return nil
}

func (p *Provider) createRemoteExecContainer() (string, error) {
	// generate the ID for the new container based on the clock time and a string
	fqdn := utils.FQDN(p.config.Name, p.config.Module, p.config.Type)

	new := contTypes.Container{
		Name:        fqdn,
		Image:       &contTypes.Image{Name: p.config.Image.Name, Username: p.config.Image.Username, Password: p.config.Image.Password},
		Environment: p.config.Environment,
	}

	for _, v := range p.config.Networks {
		new.Networks = append(new.Networks, types.NetworkAttachment{
			ID:        v.ID,
			Name:      v.Name,
			IPAddress: v.IPAddress,
			Aliases:   v.Aliases,
		})
	}

	for _, v := range p.config.Volumes {
		new.Volumes = append(new.Volumes, types.Volume{
			Source:                      v.Source,
			Destination:                 v.Destination,
			Type:                        v.Type,
			ReadOnly:                    v.ReadOnly,
			BindPropagation:             v.BindPropagation,
			BindPropagationNonRecursive: v.BindPropagationNonRecursive,
			SelinuxRelabel:              v.SelinuxRelabel,
		})
	}

	new.Entrypoint = []string{}
	new.Command = []string{"tail", "-f", "/dev/null"} // ensure container does not immediately exit

	// pull any images needed for this container
	err := p.container.PullImage(*new.Image, false)
	if err != nil {
		p.log.Error("error pulling container image", "ref", p.config.ID, "image", new.Image.Name)

		return "", err
	}

	id, err := p.container.CreateContainer(&new)
	if err != nil {
		p.log.Error("error creating container for remote exec", "ref", p.config.Name, "image", p.config.Image, "networks", p.config.Networks, "volumes", p.config.Volumes)
		return "", err
	}

	return id, err
}

func (p *Provider) createLocalExec() (int, error) {
	// depending on the OS, we might need to replace line endings
	// just in case the script was created on a different OS
	contents := p.config.Script
	if runtime.GOOS != "windows" {
		contents = strings.Replace(contents, "\r\n", "\n", -1)
	}

	// create a temporary file for the script
	scriptPath := filepath.Join(utils.JumppadTemp(), fmt.Sprintf("exec_%s.sh", p.config.Name))
	err := os.WriteFile(scriptPath, []byte(contents), 0755)
	if err != nil {
		return 0, fmt.Errorf("unable to write script to file: %s", err)
	}

	// build the environment variables
	envs := []string{}

	for k, v := range p.config.Environment {
		envs = append(envs, fmt.Sprintf("%s=%s", k, v))
	}

	// create the folders for logs and pids
	logPath := filepath.Join(utils.LogsDir(), fmt.Sprintf("exec_%s.log", p.config.Name))

	// do we have a duration to parse
	var d time.Duration
	if p.config.Timeout != "" {
		d, err = time.ParseDuration(p.config.Timeout)
		if err != nil {
			return 0, fmt.Errorf("unable to parse duration for timeout: %s", err)
		}

		if p.config.Daemon {
			p.log.Warn("timeout will be ignored when exec is running in daemon mode")
		}
	}

	// create the config
	cc := cmdTypes.CommandConfig{
		Command:          scriptPath,
		Env:              envs,
		WorkingDirectory: p.config.WorkingDirectory,
		RunInBackground:  p.config.Daemon,
		LogFilePath:      logPath,
		Timeout:          d,
	}

	pid, err := p.command.Execute(cc)
	if err != nil {
		return 0, err
	}

	return pid, nil
}
