package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"github.com/pockyHM/conan/pkg/configschema"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var ErrAuthFailed = errors.New("ssh authentication failed")

type Target struct {
	Host             string
	SSHPort          int
	Username         string
	Password         string
	AgentPort        int
	Token            string
	AgentBinOverride string
	Config           configschema.AgentDeployConfig
	KnownHostsPath   string
}

type Remote interface {
	Run(ctx context.Context, command string, stdin string) (string, error)
	Upload(ctx context.Context, remotePath string, contents []byte, perm os.FileMode) error
}

type Connector func(Target) (Remote, error)

type Deployer struct {
	connect Connector
}

func NewDeployer(connect Connector) *Deployer {
	return &Deployer{connect: connect}
}

func NewNativeDeployer() *Deployer {
	return NewDeployer(connectNative)
}

func (d *Deployer) Deploy(ctx context.Context, target Target) error {
	if target.SSHPort == 0 {
		target.SSHPort = 22
	}
	remote, err := d.connect(target)
	if err != nil {
		return err
	}
	uname, err := remote.Run(ctx, "uname -m", "")
	if err != nil {
		return err
	}
	arch, err := ArchFromUname(uname)
	if err != nil {
		return err
	}
	binaryPath, err := ResolveAgentBinary(target.Config, arch, target.AgentBinOverride)
	if err != nil {
		return err
	}
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		return err
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	remoteBinaryTmp := "/tmp/conan-agent." + suffix
	remoteConfigTmp := "/tmp/conan-agent-config." + suffix
	remoteUnitTmp := "/tmp/conan-agent.service." + suffix
	if err := remote.Upload(ctx, remoteBinaryTmp, binary, 0755); err != nil {
		return err
	}
	if err := remote.Upload(ctx, remoteConfigTmp, []byte(RenderAgentConfig(target.AgentPort, target.Token)), 0600); err != nil {
		return err
	}
	if err := remote.Upload(ctx, remoteUnitTmp, []byte(RenderSystemdUnit(target.Config.RemoteBinaryPath, target.Config.RemoteConfigPath)), 0644); err != nil {
		return err
	}

	commands := []string{
		fmt.Sprintf("sudo -S install -m 0755 %s %s", remoteBinaryTmp, shellQuote(target.Config.RemoteBinaryPath)),
		fmt.Sprintf("sudo -S mkdir -p %s", shellQuote(filepath.Dir(target.Config.RemoteConfigPath))),
		fmt.Sprintf("sudo -S install -m 0600 %s %s", remoteConfigTmp, shellQuote(target.Config.RemoteConfigPath)),
		fmt.Sprintf("sudo -S install -m 0644 %s %s", remoteUnitTmp, shellQuote(target.Config.SystemdUnitPath)),
		"sudo -S systemctl daemon-reload",
		"sudo -S systemctl enable --now conan-agent",
		"sudo -S systemctl restart conan-agent",
	}
	stdin := target.Password + "\n"
	for _, command := range commands {
		if _, err := remote.Run(ctx, command, stdin); err != nil {
			return err
		}
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

type sshRemote struct {
	client *ssh.Client
}

func connectNative(target Target) (Remote, error) {
	hostKeyCallback, err := hostKeyCallback(target.KnownHostsPath)
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User:            target.Username,
		Auth:            []ssh.AuthMethod{ssh.Password(target.Password)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         15 * time.Second,
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", target.Host, target.SSHPort), config)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unable to authenticate") {
			return nil, ErrAuthFailed
		}
		return nil, err
	}
	return &sshRemote{client: client}, nil
}

func hostKeyCallback(knownHostsPath string) (ssh.HostKeyCallback, error) {
	if knownHostsPath == "" {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(knownHostsPath), 0700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(knownHostsPath, nil, 0600); err != nil {
			return nil, err
		}
	}
	cb, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			return appendKnownHost(knownHostsPath, hostname, remote, key)
		}
		return err
	}, nil
}

func appendKnownHost(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	_, err = fmt.Fprintln(f, line)
	return err
}

func (r *sshRemote) Run(ctx context.Context, command string, stdin string) (string, error) {
	session, err := r.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	if stdin != "" {
		session.Stdin = strings.NewReader(stdin)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return "", ctx.Err()
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("remote command failed: %s: %s", command, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), nil
	}
}

func (r *sshRemote) Upload(ctx context.Context, remotePath string, contents []byte, perm os.FileMode) error {
	client, err := sftp.NewClient(r.client)
	if err != nil {
		return err
	}
	defer client.Close()
	file, err := client.OpenFile(remotePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return client.Chmod(remotePath, perm)
}
