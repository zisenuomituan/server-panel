// Package sshpool 维护到各目标机的 SSH 长连接，避免每次采集都重新握手。
package sshpool

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// KnownHosts 记录已知的 SSH 主机密钥，用于第一次信任（TOFU）之后校验。
type KnownHosts interface {
	HostKey(host string) (key string, ok bool, err error)
	SaveHostKey(host, key string) error
}

type Pool struct {
	keyPath string
	timeout time.Duration
	known   KnownHosts

	mu      sync.Mutex
	clients map[string]*ssh.Client
}

func New(keyPath string) *Pool {
	return &Pool{
		keyPath: keyPath,
		timeout: 10 * time.Second,
		clients: make(map[string]*ssh.Client),
	}
}

// UseKnownHosts 打开主机密钥校验；不调用则等同于信任所有主机。
func (p *Pool) UseKnownHosts(kh KnownHosts) {
	p.known = kh
}

// CheckTOFU 第一次见到某主机就记住它的密钥，之后必须一致，否则报错。
func CheckTOFU(kh KnownHosts, hostname string, key ssh.PublicKey) error {
	enc := key.Type() + " " + base64.StdEncoding.EncodeToString(key.Marshal())
	saved, ok, err := kh.HostKey(hostname)
	if err != nil {
		return err
	}
	if !ok {
		return kh.SaveHostKey(hostname, enc)
	}
	if saved != enc {
		return fmt.Errorf("主机 %s 的 SSH 密钥和记录不一致，可能有中间人攻击", hostname)
	}
	return nil
}

func (p *Pool) signer() (ssh.AuthMethod, error) {
	if p.keyPath == "" {
		return nil, fmt.Errorf("没有配置 SSH 私钥")
	}
	raw, err := os.ReadFile(p.keyPath)
	if err != nil {
		return nil, fmt.Errorf("读私钥失败: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败: %w", err)
	}
	return ssh.PublicKeys(signer), nil
}

func (p *Pool) client(addr, user string) (*ssh.Client, error) {
	key := user + "@" + addr

	p.mu.Lock()
	c, ok := p.clients[key]
	p.mu.Unlock()
	if ok {
		return c, nil
	}

	auth, err := p.signer()
	if err != nil {
		return nil, err
	}
	hostKeyCB := ssh.InsecureIgnoreHostKey()
	if p.known != nil {
		known := p.known
		hostKeyCB = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return CheckTOFU(known, hostname, key)
		}
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: hostKeyCB,
		Timeout:         p.timeout,
	}
	c, err = ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.clients[key] = c
	p.mu.Unlock()
	return c, nil
}

func (p *Pool) drop(addr, user string) {
	p.mu.Lock()
	if c, ok := p.clients[user+"@"+addr]; ok {
		c.Close()
		delete(p.clients, user+"@"+addr)
	}
	p.mu.Unlock()
}

// Run 在远端执行一条命令并返回 stdout。连接失效会自动重连一次。
func (p *Pool) Run(ctx context.Context, addr, user, command string) (string, error) {
	out, err := p.runOnce(addr, user, command)
	if err != nil {
		p.drop(addr, user)
		out, err = p.runOnce(addr, user, command)
	}
	return out, err
}

func (p *Pool) runOnce(addr, user, command string) (string, error) {
	c, err := p.client(addr, user)
	if err != nil {
		return "", err
	}
	session, err := c.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	if err := session.Run(command); err != nil {
		msg := stderr.String()
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

// RunOutput 执行命令并允许非零退出码，用于探测类命令。
func (p *Pool) RunOutput(addr, user, command string) (string, error) {
	c, err := p.client(addr, user)
	if err != nil {
		return "", err
	}
	session, err := c.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var stdout bytes.Buffer
	session.Stdout = &stdout
	_ = session.Run(command)
	return stdout.String(), nil
}

// RunShell 执行用户输入的命令，合并 stdout/stderr，返回退出码。超时会杀掉会话。
func (p *Pool) RunShell(addr, user, command string, timeout time.Duration) (string, int, error) {
	c, err := p.client(addr, user)
	if err != nil {
		return "", -1, err
	}
	session, err := c.NewSession()
	if err != nil {
		return "", -1, err
	}
	defer session.Close()

	out := &syncBuffer{}
	session.Stdout = out
	session.Stderr = out

	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()

	select {
	case err := <-done:
		if err == nil {
			return out.String(), 0, nil
		}
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			return out.String(), exitErr.ExitStatus(), nil
		}
		return out.String(), -1, err
	case <-time.After(timeout):
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		return out.String(), -1, fmt.Errorf("命令执行超时")
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Close 关掉所有连接，进程退出时调用。
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, c := range p.clients {
		c.Close()
		delete(p.clients, k)
	}
}
