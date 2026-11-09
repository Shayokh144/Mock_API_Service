// Command mockproxy is a local mock API proxy for one developer machine.
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/Shayokh144/Mock_API_Service/internal/admin"
	"github.com/Shayokh144/Mock_API_Service/internal/config"
	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/proxy"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
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
	ups, err := upstreams.Load(cfg.UpstreamsPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return err
	}
	if cfg.Upstream != nil {
		if err := ups.Seed(cfg.Upstream.String()); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return err
		}
	}
	log := history.New(cfg.HistoryLimit)
	dashboard, err := admin.New(store, log, ups)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return err
	}
	handler := proxy.New(proxy.Options{
		Upstreams: ups,
		Timeout:   cfg.UpstreamTimeout,
		Rules:     store,
		History:   log,
		Admin:     dashboard,
	})
	return listen(cfg.ListenAddr, handler)
}
