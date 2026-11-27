// Command Velan launches either the MyGo native desktop player or, when
// invoked with --gateway, the isolated VelanGateway child process.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/xiangkunzhang/Velan/internal/app/shell"
	"github.com/xiangkunzhang/Velan/internal/gateway/server"
)

//go:embed mygo.json
var mygoManifest []byte

func resolveAppVersion() string {
	var cfg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(mygoManifest, &cfg); err == nil {
		if v := strings.TrimSpace(cfg.Version); v != "" {
			return v
		}
	}
	return "dev"
}

func main() {
	gatewayMode := flag.Bool("gateway", false, "run as isolated VelanGateway subprocess")
	gatewayPort := flag.Int("port", 0, "gateway listen port (0 = random)")
	flag.Parse()

	if *gatewayMode {
		if err := server.Run(context.Background(), server.Options{
			Port:       *gatewayPort,
			WatchStdin: true,
		}); err != nil {
			log.Printf("gateway exit: %v", err)
			os.Exit(1)
		}
		return
	}

	shell.Run(resolveAppVersion(), flag.Args())
}
