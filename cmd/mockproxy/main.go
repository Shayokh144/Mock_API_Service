// Command mockproxy is a local mock API proxy for one developer machine.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/Shayokh144/Mock_API_Service/internal/admin"
	"github.com/Shayokh144/Mock_API_Service/internal/api"
	"github.com/Shayokh144/Mock_API_Service/internal/config"
	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/proxy"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/scenario"
	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

var (
	scenarioFile    string
	scenarioPersist bool
)

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&scenarioFile, "scenario", "", "YAML or JSON scenario to merge into the default session at startup")
	fs.BoolVar(&scenarioPersist, "scenario-persist", false, "write that scenario into the rules file")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if err := run(os.Getenv, os.Stderr, listen); err != nil {
		os.Exit(1)
	}
}

func listen(addr string, handler http.Handler) error {
	return httpServer(addr, handler).ListenAndServe()
}

func httpServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
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
	if err := loadStartupScenario(stderr, store, ups); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return err
	}
	log := history.New(cfg.HistoryLimit)
	dashboard, err := admin.New(store, log, ups)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return err
	}
	dashboard.UseListenAddr(cfg.AdminListenAddr)
	dashboard.UseProxyAddr(cfg.ListenAddr)
	manager := sessions.New(sessions.Options{
		Rules:        store,
		History:      log,
		Upstreams:    ups,
		ProxyAddr:    cfg.ListenAddr,
		AdminAddr:    cfg.AdminListenAddr,
		Timeout:      cfg.UpstreamTimeout,
		HistoryLimit: cfg.HistoryLimit,
	})
	defer manager.Close()
	dashboard.UseSessions(manager)
	tokens, err := api.LoadToken(cfg.TokenPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return err
	}
	apiServer := api.New(tokens, cfg.AdminListenAddr, api.Version)
	apiServer.UseSessions(manager)
	adminHandler := api.Mount(dashboard, apiServer)
	handler := proxy.New(proxy.Options{
		Upstreams: ups,
		Timeout:   cfg.UpstreamTimeout,
		Rules:     store,
		History:   log,
	})
	return listenBoth(listen, cfg.AdminListenAddr, adminHandler, cfg.ListenAddr, handler)
}

func loadStartupScenario(stderr io.Writer, store *rules.Store, ups *upstreams.Store) error {
	if scenarioFile == "" {
		return nil
	}
	data, err := os.ReadFile(scenarioFile)
	if err != nil {
		return err
	}
	doc, err := scenario.Parse(data, scenario.Format(scenarioFile, ""))
	if err != nil {
		return err
	}
	drafts, err := doc.Drafts(func(name string) (string, error) {
		name = strings.TrimSpace(name)
		if name == "" || strings.EqualFold(name, "any") {
			return "", nil
		}
		for _, item := range ups.List() {
			if item.Name == name {
				return item.ID, nil
			}
		}
		return "", fmt.Errorf("choose an upstream or Any upstream")
	})
	if err != nil {
		return err
	}
	if _, err := store.ApplyDrafts(drafts, false, scenarioPersist); err != nil {
		return err
	}
	fmt.Fprintln(stderr, "loaded scenario "+scenarioFile)
	return nil
}

func listenBoth(listen func(string, http.Handler) error, adminAddr string, admin http.Handler, proxyAddr string, proxyHandler http.Handler) error {
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	start := func(addr string, handler http.Handler) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := listen(addr, handler); err != nil {
				errCh <- err
			}
		}()
	}
	start(adminAddr, admin)
	start(proxyAddr, proxyHandler)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case err := <-errCh:
		return err
	case <-done:
		return nil
	}
}
