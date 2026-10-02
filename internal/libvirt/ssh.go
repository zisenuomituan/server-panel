package libvirt

import (
	"context"
	"fmt"
	"strings"

	"serverpanel/internal/sshpool"
)

// SSHBackend 通过 SSH 到 KVM 宿主机执行 virsh 命令。
type SSHBackend struct {
	pool *sshpool.Pool
	addr string
	user string
	uri  string
}

func NewSSH(pool *sshpool.Pool, host string, port int, user, uri string) *SSHBackend {
	if user == "" {
		user = "root"
	}
	if uri == "" {
		uri = "qemu:///system"
	}
	return &SSHBackend{
		pool: pool,
		addr: fmt.Sprintf("%s:%d", host, port),
		user: user,
		uri:  uri,
	}
}

func (b *SSHBackend) virsh(args ...string) string {
	// 统一用 C locale，避免目标机是中文环境时解析不了输出
	return "LC_ALL=C virsh -c " + shQuote(b.uri) + " " + strings.Join(args, " ")
}

func (b *SSHBackend) run(ctx context.Context, command string) (string, error) {
	return b.pool.Run(ctx, b.addr, b.user, command)
}

func (b *SSHBackend) List(ctx context.Context) ([]Domain, error) {
	out, err := b.run(ctx, b.virsh("list", "--all", "--name"))
	if err != nil {
		return nil, err
	}

	var domains []Domain
	for _, name := range strings.Split(out, "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		info, err := b.run(ctx, b.virsh("dominfo", name))
		if err != nil {
			// 单台取不到不影响其它，标记成 other
			domains = append(domains, Domain{Name: name, State: Other})
			continue
		}
		state, vcpu, memMB := parseDominfo(info)
		domains = append(domains, Domain{Name: name, State: state, VCPU: vcpu, MemMB: memMB})
	}
	return domains, nil
}

func (b *SSHBackend) Stats(ctx context.Context, name string) (Stats, error) {
	out, err := b.run(ctx, b.virsh("domstats", name))
	if err != nil {
		return Stats{}, err
	}
	return parseDomstats(out), nil
}

func (b *SSHBackend) Start(ctx context.Context, name string) error {
	return b.action(ctx, "start", name)
}

func (b *SSHBackend) Shutdown(ctx context.Context, name string) error {
	return b.action(ctx, "shutdown", name)
}

func (b *SSHBackend) ForceOff(ctx context.Context, name string) error {
	return b.action(ctx, "destroy", name)
}

func (b *SSHBackend) Reboot(ctx context.Context, name string) error {
	return b.action(ctx, "reboot", name)
}

func (b *SSHBackend) action(ctx context.Context, verb, name string) error {
	_, err := b.run(ctx, b.virsh(verb, shQuote(name)))
	return err
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
