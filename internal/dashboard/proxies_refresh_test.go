package dashboard

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

const refreshSourcePlugin = `--- @plugin Refresh Source Plugin
--- @author tester
--- @version 1.0.0
--- @router_version 0.0.4
--- @allow_host example.com
--- @proxy_source true

llm_router.register_proxy_source("testsrc", {
  fetch_proxies = function(ctx)
    return {
      { protocol = "http", host = "127.0.0.1", port = 1, country = "US" },
      { protocol = "http", host = "127.0.0.1", port = 2, country = "US" },
    }
  end,
})
`

// TestProxySourceRefreshSurvivesDeadRequestContext guards the fire-and-forget
// fetch: the background work must not inherit the request context, which
// dies with the 202 response.
func TestProxySourceRefreshSkippedWhenFull(t *testing.T) {
	database := testutil.SetupTestDB(t)
	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	installed, err := luaSvc.Install([]byte(refreshSourcePlugin), luaplugin.PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	srcKey := luaplugin.QualifiedSourceKey(installed.ID, "testsrc")
	proxySvc := proxypool.New(database)
	h := &Handler{luaSvc: luaSvc, proxySvc: proxySvc}

	// Narrow demand to one region and fill it: the gate must short-circuit.
	oldDefaults := proxypool.DefaultRegions
	proxypool.DefaultRegions = []string{"US"}
	defer func() { proxypool.DefaultRegions = oldDefaults }()
	proxySvc.SetConfig(1, 1)

	payload := make([]byte, 1<<20)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/geo") {
			_, _ = io.WriteString(w, `{"status":"success","countryCode":"US"}`)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = w.Write(payload)
	}))
	t.Cleanup(origin.Close)
	originAddr := strings.TrimPrefix(origin.URL, "http://")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(30 * time.Second))
				br := bufio.NewReader(c)
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}
				for {
					h, err := br.ReadString('\n')
					if err != nil || h == "\r\n" || h == "\n" {
						break
					}
				}
				if strings.HasPrefix(line, "CONNECT") {
					_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\n\r\n")
					return
				}
				parts := strings.Split(line, " ")
				if len(parts) < 2 {
					return
				}
				target := strings.TrimPrefix(parts[1], "http://"+originAddr)
				oc, err := net.Dial("tcp", originAddr)
				if err != nil {
					return
				}
				defer oc.Close()
				_, _ = fmt.Fprintf(oc, "GET %s HTTP/1.0\r\nHost: %s\r\nConnection: close\r\n\r\n", target, originAddr)
				_, _ = io.Copy(c, oc)
			}(conn)
		}
	}()
	proxySvc.CheckURL = "http://" + originAddr + "/file"
	proxySvc.HandshakeHost = ln.Addr().String()
	if _, err := proxySvc.AddManual("http://"+ln.Addr().String(), "US"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if proxySvc.NeedsSearch() {
		t.Fatal("pool must read as full")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/proxy-sources/refresh?key="+url.QueryEscape(srcKey), nil)
	rec := httptest.NewRecorder()
	h.apiProxySourceRefresh(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status: got %d, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"started":false`) {
		t.Fatalf("full pool must not start refresh: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "pool full") {
		t.Fatalf("response must carry the reason: %s", rec.Body.String())
	}
}

func TestProxySourceRefreshSurvivesDeadRequestContext(t *testing.T) {
	database := testutil.SetupTestDB(t)
	luaSvc, err := luaplugin.New(database, nil)
	if err != nil {
		t.Fatalf("lua service: %v", err)
	}
	installed, err := luaSvc.Install([]byte(refreshSourcePlugin), luaplugin.PluginOrigin{Manual: true})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	srcKey := luaplugin.QualifiedSourceKey(installed.ID, "testsrc")
	h := &Handler{luaSvc: luaSvc, proxySvc: proxypool.New(database)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/llm-router/dashboard/proxy-sources/refresh?key="+url.QueryEscape(srcKey), nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.apiProxySourceRefresh(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status: got %d, body %s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		infos := h.proxySvc.SourceInfos([]string{srcKey})
		if len(infos) == 1 && infos[0].Status == proxypool.SourceStatusIdle {
			if infos[0].LastError != "" {
				t.Fatalf("background fetch must not fail: %s", infos[0].LastError)
			}
			if infos[0].Total != 2 {
				t.Fatalf("fetch must complete all candidates, total=%d", infos[0].Total)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("source never settled: %+v", infos)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
