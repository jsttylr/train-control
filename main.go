// Train Control: a loco roster and inventory for a DCC layout, served as a web page on your network.
// Track control (JMRI / LocoNet) will be added here later.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func main() {
	dataDir := flag.String("data", env("TRAIN_CONTROL_DATA", "data"), "folder for the roster file")
	listen := flag.String("listen", env("TRAIN_CONTROL_LISTEN", "0.0.0.0:8070"), "address and port for the web page")
	noBrowser := flag.Bool("no-browser", env("TRAIN_CONTROL_NO_BROWSER", "") != "", "don't open the page in a browser at start")
	health := flag.Bool("healthcheck", false, "check that a running copy answers, for Docker")
	flag.Parse()

	if *health {
		os.Exit(healthcheck(*listen))
	}

	locos, err := OpenStore[Loco](*dataDir, "locos.json", locoLess)
	if err != nil {
		log.Fatalf("can't open the roster in %s: %v", *dataDir, err)
	}
	items, err := OpenStore[Item](*dataDir, "inventory.json", itemLess)
	if err != nil {
		log.Fatalf("can't open the inventory in %s: %v", *dataDir, err)
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("can't listen on %s: %v (is another copy already running?)", *listen, err)
	}
	srv := &http.Server{Handler: routes(stores{locos, items}), ReadHeaderTimeout: 10 * time.Second}
	url := pageURL(*listen)
	log.Printf("Train Control is running at %s (data in %s)", url, *dataDir)
	if !*noBrowser {
		openBrowser(url)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func pageURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://" + listen + "/"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port) + "/"
}

func healthcheck(listen string) int {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(strings.TrimSuffix(pageURL(listen), "/") + "/api/health")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err == nil {
		go cmd.Wait()
	}
}
