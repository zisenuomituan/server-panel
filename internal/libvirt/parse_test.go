package libvirt

import "testing"

func TestParseDominfo(t *testing.T) {
	out := `Id:             1
Name:           test
UUID:           6695eb01-f6a4
OS Type:        hvm
State:          running
CPU(s):         2
Max memory:     2097152 KiB
Used memory:    2097152 KiB
`
	state, vcpu, memMB := parseDominfo(out)
	if state != Running {
		t.Errorf("state = %q", state)
	}
	if vcpu != 2 {
		t.Errorf("vcpu = %d", vcpu)
	}
	if memMB != 2048 {
		t.Errorf("memMB = %d", memMB)
	}
}

func TestParseDominfoShutoff(t *testing.T) {
	out := "State:          shut off\nCPU(s):         1\nMax memory:     1048576 KiB\n"
	state, vcpu, memMB := parseDominfo(out)
	if state != Shutoff || vcpu != 1 || memMB != 1024 {
		t.Fatalf("解析结果不对: %v %d %d", state, vcpu, memMB)
	}
}

func TestParseDomstats(t *testing.T) {
	out := `Domain: 'test'
  state.state=1
  cpu.time=1790865600700000000
  memory.total=8388608
  balloon.rss=2097152
  net.count=1
  net.0.name=vnet0
  net.0.rx.bytes=1000
  net.0.tx.bytes=2000
  block.count=1
  block.0.rd.bytes=3000
  block.0.wr.bytes=4000
`
	st := parseDomstats(out)
	if st.CPUSeconds < 1790865599 || st.CPUSeconds > 1790865601 {
		t.Errorf("CPUSeconds = %f", st.CPUSeconds)
	}
	if st.MemTotalMB != 8192 {
		t.Errorf("MemTotalMB = %d", st.MemTotalMB)
	}
	if st.MemUsedMB != 2048 {
		t.Errorf("MemUsedMB = %d", st.MemUsedMB)
	}
	if st.NetRx != 1000 || st.NetTx != 2000 {
		t.Errorf("网络: rx=%d tx=%d", st.NetRx, st.NetTx)
	}
	if st.BlockRead != 3000 || st.BlockWrite != 4000 {
		t.Errorf("磁盘: rd=%d wr=%d", st.BlockRead, st.BlockWrite)
	}
}
