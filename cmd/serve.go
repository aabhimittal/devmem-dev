/*
Copyright © 2026 Surya Parida
*/
package cmd

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"github.com/yourusername/devmem/internal/server"
)

var (
	servePort   int
	serveNoOpen bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Launch a local web dashboard for DevMem documentation",
	Long: `Serve renders the generated DevMem documentation as a browsable dashboard:
project overview, module cards, the master architecture diagram, and the
per-commit changelog timeline.

It is read-only and fully offline (no API key required) and reads .devmem/ fresh
on every request, so changes captured while the server is running appear on a
browser refresh.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		addr := fmt.Sprintf("127.0.0.1:%d", servePort)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("listen on %s: %w (is the port already in use? try --port)", addr, err)
		}

		url := fmt.Sprintf("http://localhost:%d", servePort)
		srv := &http.Server{
			Handler:           server.New(repoDir).Handler(),
			ReadHeaderTimeout: 10 * time.Second,
		}

		fmt.Fprintf(os.Stderr, "DevMem dashboard serving %s\n", repoDir)
		fmt.Fprintf(os.Stderr, "  %s  (press Ctrl+C to stop)\n", url)

		if !serveNoOpen {
			openBrowser(url)
		}

		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve dashboard: %w", err)
		}
		return nil
	},
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 7777, "Port to serve the dashboard on")
	serveCmd.Flags().BoolVar(&serveNoOpen, "no-open", false, "Do not open the dashboard in a browser automatically")
	rootCmd.AddCommand(serveCmd)
}

// openBrowser attempts to open url in the default browser. Failures are ignored
// since the URL is always printed to the terminal.
func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	if _, err := exec.LookPath(cmd); err != nil {
		return
	}
	_ = exec.Command(cmd, args...).Start()
}
