package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRunRejectsNonLoopbackBeforeListen(t *testing.T) {
	called := false
	err := run(func(key string) string {
		switch key {
		case "UPSTREAM_BASE_URL":
			return "http://127.0.0.1:9"
		case "LISTEN_ADDR":
			return "0.0.0.0:8080"
		default:
			return ""
		}
	}, io.Discard, func(string, http.Handler) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("listen was called")
	}
	if !strings.Contains(err.Error(), "LISTEN_ADDR") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunLogsListenAddressAndDashboard(t *testing.T) {
	var stderr bytes.Buffer
	var gotAddr string
	err := run(func(key string) string {
		if key == "UPSTREAM_BASE_URL" {
			return "https://ruby-dev.example.com"
		}
		return ""
	}, &stderr, func(addr string, h http.Handler) error {
		gotAddr = addr
		if h == nil {
			t.Fatal("nil handler")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotAddr != "127.0.0.1:8080" {
		t.Fatalf("addr = %s", gotAddr)
	}
	text := stderr.String()
	if !strings.Contains(text, "listening on 127.0.0.1:8080") {
		t.Fatalf("stderr = %q", text)
	}
	if !strings.Contains(text, "dashboard at http://localhost:8080/_admin") {
		t.Fatalf("stderr = %q", text)
	}
}

func TestRunMissingUpstreamDoesNotListen(t *testing.T) {
	called := false
	err := run(func(string) string { return "" }, io.Discard, func(string, http.Handler) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	if !strings.Contains(err.Error(), "UPSTREAM_BASE_URL") {
		t.Fatalf("error = %v", err)
	}
}
