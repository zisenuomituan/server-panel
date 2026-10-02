// vm-collect 跑在被监控的机器上，被 center 通过 SSH 调用，输出一行 JSON 指标。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"serverpanel/internal/collector"
)

func main() {
	pretty := flag.Bool("pretty", false, "输出带缩进的 JSON，方便调试")
	flag.Parse()

	r, err := collector.Collect()
	if err != nil {
		fmt.Fprintln(os.Stderr, "采集失败:", err)
		os.Exit(1)
	}

	var out []byte
	if *pretty {
		out, _ = json.MarshalIndent(r, "", "  ")
	} else {
		out, _ = json.Marshal(r)
	}
	os.Stdout.Write(out)
}
