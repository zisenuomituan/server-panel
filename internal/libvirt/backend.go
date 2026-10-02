// Package libvirt 把"操作 KVM 宿主机"抽象成接口，方便开发和测试时用假实现。
package libvirt

import "context"

type State string

const (
	Running State = "running"
	Shutoff State = "shut off"
	Paused  State = "paused"
	Other   State = "other"
)

type Domain struct {
	Name  string
	State State
	VCPU  int
	MemMB int
}

// Stats 是宿主机侧通过 libvirt 能看到的东西，比 guest 内采集粗一些。
type Stats struct {
	CPUSeconds float64
	MemUsedMB  int64
	MemTotalMB int64
	NetRx      uint64
	NetTx      uint64
	BlockRead  uint64
	BlockWrite uint64
}

type Backend interface {
	List(ctx context.Context) ([]Domain, error)
	Stats(ctx context.Context, name string) (Stats, error)
	Start(ctx context.Context, name string) error
	Shutdown(ctx context.Context, name string) error
	ForceOff(ctx context.Context, name string) error
	Reboot(ctx context.Context, name string) error
}

func ParseState(s string) State {
	switch s {
	case "running":
		return Running
	case "shut off", "shutoff", "shutdown":
		return Shutoff
	case "paused":
		return Paused
	default:
		return Other
	}
}
