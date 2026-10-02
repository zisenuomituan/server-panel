package libvirt

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// FakeBackend 用来在宿主机不可用时跑通整条链路。指标是模拟出来的，
// 但状态机（开机/关机）是真实的。
type FakeBackend struct {
	mu      sync.Mutex
	domains map[string]*fakeDomain
	order   []string
}

type fakeDomain struct {
	state      State
	vcpu       int
	memMB      int
	startedAt  time.Time
	lastTick   time.Time
	cpuSeconds float64
	netRx      uint64
	netTx      uint64
	jitter     float64

	// 模拟重启/开机时的短暂过渡，让界面能看出状态在变
	bootUntil time.Time
}

func NewFake() *FakeBackend {
	f := &FakeBackend{domains: map[string]*fakeDomain{}}
	f.add("web-01", 2, 2048, Running)
	f.add("db-01", 4, 4096, Running)
	f.add("cache-01", 1, 1024, Shutoff)
	return f
}

func (f *FakeBackend) add(name string, vcpu, memMB int, state State) {
	now := time.Now()
	d := &fakeDomain{
		state:     state,
		vcpu:      vcpu,
		memMB:     memMB,
		startedAt: now,
		lastTick:  now,
		jitter:    rand.Float64(),
	}
	f.domains[name] = d
	f.order = append(f.order, name)
}

func (f *FakeBackend) List(ctx context.Context) ([]Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]Domain, 0, len(f.order))
	for _, name := range f.order {
		d := f.domains[name]
		state := d.state
		if state == Running && !d.bootUntil.IsZero() && time.Now().Before(d.bootUntil) {
			state = Paused // 界面显示成"重启中"
		}
		out = append(out, Domain{Name: name, State: state, VCPU: d.vcpu, MemMB: d.memMB})
	}
	return out, nil
}

func (f *FakeBackend) Stats(ctx context.Context, name string) (Stats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	d, ok := f.domains[name]
	if !ok {
		return Stats{}, fmt.Errorf("没有这台虚拟机: %s", name)
	}
	if d.state != Running {
		return Stats{MemTotalMB: int64(d.memMB)}, nil
	}

	now := time.Now()
	dt := now.Sub(d.lastTick).Seconds()
	if dt <= 0 {
		dt = 1
	}
	d.lastTick = now

	// 让占用在一个区间里缓慢漂移，看起来像真的
	d.jitter += (rand.Float64() - 0.5) * 0.1
	if d.jitter < 0.1 {
		d.jitter = 0.1
	}
	if d.jitter > 0.9 {
		d.jitter = 0.9
	}

	usedFrac := 0.3 + d.jitter*0.45
	d.cpuSeconds += usedFrac * dt * float64(d.vcpu)

	rx := uint64((0.4 + d.jitter) * 800 * 1024 * dt)
	tx := uint64((0.3 + (1 - d.jitter)) * 500 * 1024 * dt)
	d.netRx += rx
	d.netTx += tx

	return Stats{
		CPUSeconds: d.cpuSeconds,
		MemUsedMB:  int64(float64(d.memMB) * (0.4 + d.jitter*0.4)),
		MemTotalMB: int64(d.memMB),
		NetRx:      d.netRx,
		NetTx:      d.netTx,
	}, nil
}

func (f *FakeBackend) Start(ctx context.Context, name string) error {
	return f.set(name, Running)
}

func (f *FakeBackend) Shutdown(ctx context.Context, name string) error {
	return f.set(name, Shutoff)
}

func (f *FakeBackend) ForceOff(ctx context.Context, name string) error {
	return f.set(name, Shutoff)
}

func (f *FakeBackend) Reboot(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.domains[name]
	if !ok {
		return fmt.Errorf("没有这台虚拟机: %s", name)
	}
	d.startedAt = time.Now()
	d.lastTick = time.Now()
	d.state = Running
	d.bootUntil = time.Now().Add(3 * time.Second)
	return nil
}

func (f *FakeBackend) set(name string, state State) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.domains[name]
	if !ok {
		return fmt.Errorf("没有这台虚拟机: %s", name)
	}
	now := time.Now()
	d.state = state
	if state == Running {
		d.startedAt = now
		d.bootUntil = now.Add(3 * time.Second)
	} else {
		d.bootUntil = time.Time{}
	}
	d.lastTick = now
	return nil
}
