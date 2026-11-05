// Command mockproxy is a local mock API proxy for one developer machine.
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/Shayokh144/Mock_API_Service/internal/config"
	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/proxy"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func main() {
	if err := run(os.Getenv, os.Stderr, http.ListenAndServe); err != nil {
		os.Exit(1)
	}
}

func run(getenv func(string) string, stderr io.Writer, listen func(string, http.Handler) error) error {
	logger := log.New(stderr, "", 0)
	cfg, err := config.Load(getenv)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return err
	}
	for _, line := range config.StartupLines(cfg) {
		logger.Println(line)
	}
	store, err := rules.Load(cfg.RulesPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return err
	}
	handler := proxy.New(proxy.Options{
		Upstream: cfg.Upstream,
		Timeout:  cfg.UpstreamTimeout,
		Rules:    store,
		History:  history.New(cfg.HistoryLimit),
	})
	return listen(cfg.ListenAddr, handler)
}
