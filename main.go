// Command EkkoVideo launches either the MyGo native desktop player or, when
// invoked with --gateway, the isolated EkkoGateway child process.
package main

import (
	"context"
	_ "embed"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/xiangkunzhang/EkkoVideo/internal/app/shell"
	"github.com/xiangkunzhang/EkkoVideo/internal/gateway/server"
)

//go:embed VERSION
var appVersion string

func main() {
	gatewayMode := flag.Bool("gateway", false, "run as isolated EkkoGateway subprocess")
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

	shell.Run(strings.TrimSpace(appVersion), flag.Args())
}
