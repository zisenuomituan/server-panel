package libvirt

import (
	"strconv"
	"strings"
)

// parseDominfo 解析 `virsh dominfo <name>` 的输出。
func parseDominfo(out string) (state State, vcpu int, memMB int) {
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "State":
			state = ParseState(val)
		case "CPU(s)":
			vcpu, _ = strconv.Atoi(val)
		case "Max memory":
			// 单位是 KiB
			fields := strings.Fields(val)
			if len(fields) > 0 {
				if kib, err := strconv.Atoi(fields[0]); err == nil {
					memMB = kib / 1024
				}
			}
		}
	}
	return
}

// parseDomstats 解析 `virsh domstats <name>` 的 key=value 输出。
func parseDomstats(out string) Stats {
	var st Stats
	var rx, tx, rd, wr uint64

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch {
		case key == "cpu.time":
			if ns, err := strconv.ParseFloat(val, 64); err == nil {
				st.CPUSeconds = ns / 1e9
			}
		case key == "memory.total":
			if kib, err := strconv.ParseUint(val, 10, 64); err == nil {
				st.MemTotalMB = int64(kib / 1024)
			}
		case key == "balloon.rss" || key == "balloon.current":
			if st.MemUsedMB == 0 {
				if kib, err := strconv.ParseUint(val, 10, 64); err == nil {
					st.MemUsedMB = int64(kib / 1024)
				}
			}
		case strings.HasSuffix(key, ".rx.bytes"):
			n, _ := strconv.ParseUint(val, 10, 64)
			rx += n
		case strings.HasSuffix(key, ".tx.bytes"):
			n, _ := strconv.ParseUint(val, 10, 64)
			tx += n
		case strings.HasSuffix(key, ".rd.bytes"):
			n, _ := strconv.ParseUint(val, 10, 64)
			rd += n
		case strings.HasSuffix(key, ".wr.bytes"):
			n, _ := strconv.ParseUint(val, 10, 64)
			wr += n
		}
	}
	st.NetRx, st.NetTx = rx, tx
	st.BlockRead, st.BlockWrite = rd, wr
	return st
}
