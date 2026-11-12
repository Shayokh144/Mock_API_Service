// Command mockproxy is a local mock API proxy for one developer machine.
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/Shayokh144/Mock_API_Service/internal/admin"
	"github.com/Shayokh144/Mock_API_Service/internal/config"
	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/proxy"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func main() {
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
	log := history.New(cfg.HistoryLimit)
	dashboard, err := admin.New(store, log, ups)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return err
	}
	dashboard.UseListenAddr(cfg.AdminListenAddr)
	dashboard.UseProxyAddr(cfg.ListenAddr)
	handler := proxy.New(proxy.Options{
		Upstreams: ups,
		Timeout:   cfg.UpstreamTimeout,
		Rules:     store,
		History:   log,
	})
	return listenBoth(listen, cfg.AdminListenAddr, dashboard, cfg.ListenAddr, handler)
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
