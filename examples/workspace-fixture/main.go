// A real multi-crawl application with local-only sources and an authenticated
// proxy for browser integration tests. No scenario configuration is loaded.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/iakinsey/delver/api"
	"github.com/iakinsey/delver/application"
	ui "github.com/iakinsey/delver/web"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	root := os.Getenv("DELVER_WORKSPACE_FIXTURE_ROOT")
	var err error
	if root == "" {
		root, err = os.MkdirTemp("", "delver-workspace-browser-")
		if err != nil {
			panic(err)
		}
		defer os.RemoveAll(root)
	} else {
		if err = os.MkdirAll(root, 0700); err != nil {
			panic(err)
		}
	}
	var requestEvents struct {
		sync.Mutex
		Rows     []map[string]string
		Attempts map[string]int
	}
	var networkActivity struct {
		sync.Mutex
		Active, Peak int
	}
	var sourceURL string
	page := func(w http.ResponseWriter, r *http.Request) {
		networkActivity.Lock()
		networkActivity.Active++
		if networkActivity.Active > networkActivity.Peak {
			networkActivity.Peak = networkActivity.Active
		}
		networkActivity.Unlock()
		defer func() { networkActivity.Lock(); networkActivity.Active--; networkActivity.Unlock() }()

		outside := sourceURL + "/outside?from=" + url.QueryEscape(r.Host)
		requestEvents.Lock()
		requestEvents.Rows = append(requestEvents.Rows, map[string]string{"host": r.Host, "path": r.URL.Path, "query": r.URL.RawQuery, "user_agent": r.UserAgent(), "time_ms": fmt.Sprint(time.Now().UnixMilli())})
		attempt := 0
		if r.URL.Path == "/retry" {
			if requestEvents.Attempts == nil {
				requestEvents.Attempts = map[string]int{}
			}
			key := r.Host + r.URL.RequestURI()
			requestEvents.Attempts[key]++
			attempt = requestEvents.Attempts[key]
		}
		if len(requestEvents.Rows) > 65536 {
			requestEvents.Rows = append([]map[string]string(nil), requestEvents.Rows[len(requestEvents.Rows)-32768:]...)
		}
		requestEvents.Unlock()
		if r.URL.Path == "/retry" {
			if attempt <= 2 {
				w.WriteHeader(503)
				fmt.Fprint(w, "retry later")
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<title>Retry recovered</title><p>Recovered request.</p>")
			return
		}
		if r.URL.Path == "/stall" {
			<-r.Context().Done()
			return
		}
		if r.URL.Path == "/robots.txt" && strings.HasPrefix(r.Host, "robots-escape.") {
			http.Redirect(w, r, outside, http.StatusFound)
			return
		}
		if r.URL.Path == "/private-feed" {
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprintf(w, `<rss version="2.0"><channel><title>Private fixture</title><link>http://%s/private</link><description>Private</description><item><title>Allowed</title><link>http://%s/private-page</link></item><item><title>Outside</title><link>%s</link></item></channel></rss>`, r.Host, r.Host, outside)
			return
		}
		if r.URL.Path == "/private" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<title>Restricted graph</title><p>Private page.</p><a href="/private-page">Allowed</a><a href="%s">Outside</a><a href="/private-redirect">Redirect outside</a>`, outside)
			return
		}
		if r.URL.Path == "/private-redirect" {
			http.Redirect(w, r, outside, http.StatusFound)
			return
		}
		if r.URL.Path == "/private-page" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<title>%s</title><p>Allowed private article.</p>`, html.EscapeString(r.UserAgent()))
			return
		}
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "User-agent: *\nDisallow: /robots-denied\nAllow: /\n")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/matrix-feed/") {
			token := strings.TrimPrefix(r.URL.Path, "/matrix-feed/")
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprintf(w, `<rss version="2.0"><channel><title>Matrix</title><link>http://%s/matrix/%s</link><description>Local fixture</description><item><title>Article</title><link>http://%s/matrix/%s</link></item></channel></rss>`, html.EscapeString(r.Host), token, html.EscapeString(r.Host), token)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/matrix/") {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<title>%s</title><p>Matrix page %s</p>`, html.EscapeString(r.URL.Path), html.EscapeString(r.UserAgent()))
			if !strings.HasSuffix(r.URL.Path, "-child") {
				fmt.Fprintf(w, `<a href="%s-child">Child</a>`, html.EscapeString(r.URL.Path))
			}
			return
		}
		if r.URL.Path == "/feed" {
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprintf(w, `<rss version="2.0"><channel><title>Fixture</title><link>%s/site</link><description>Local</description><item><title>Article</title><link>%s/article</link></item></channel></rss>`, sourceURL, sourceURL)
			return
		}
		if r.URL.Path == "/feed-bad" {
			fmt.Fprint(w, "broken feed")
			return
		}
		if r.URL.Path == "/slow" {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
		if r.URL.Path == "/escape" {
			http.Redirect(w, r, strings.Replace(sourceURL, "127.0.0.1", "localhost", 1)+"/outside-redirect", http.StatusFound)
			return
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/redirect-target", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/graph" {
			fmt.Fprintf(w, `<title>Graph root</title><p>Graph root content.</p><a href="/child">Child</a><a href="%s/offsite">Outside</a><a href="/blocked">Blocked</a><a href="/redirect">Redirect</a><a href="/robots-denied">Robots denied</a><a href="%s/subdomain">Subdomain</a><a href="/escape">Escaping redirect</a>`, strings.Replace(sourceURL, "127.0.0.1", "localhost", 1), strings.Replace(sourceURL, "127.0.0.1", "sub.crawl-proxy.invalid", 1))
			return
		}
		if r.URL.Path == "/offsite" {
			fmt.Fprintf(w, `<title>Outside site</title><p>Outside content.</p><a href="%s/second-site">Second outside site</a>`, sourceURL)
			return
		}
		if r.URL.Path == "/child" {
			fmt.Fprint(w, `<title>Child</title><p>Child content.</p><a href="/grandchild">Grandchild</a>`)
			return
		}
		if r.URL.Path == "/site" {
			fmt.Fprint(w, `<title>Workspace site</title><a href="/article">Article</a>`)
		} else {
			fmt.Fprintf(w, "<title>%s</title>", r.URL.Path)
		}
	}
	source := fixtureHTTPServer(http.HandlerFunc(page), "SOURCE", false)
	defer source.Close()
	sourceURL = source.URL
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("operator:browser-proxy-secret")) {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		if r.Method == http.MethodConnect {
			host, port, err := net.SplitHostPort(r.Host)
			if err != nil || (host != "127.0.0.1" && host != "localhost") {
				http.Error(w, "local fixture only", 403)
				return
			}
			remote, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), time.Second)
			if err != nil {
				http.Error(w, "tunnel unavailable", 502)
				return
			}
			defer remote.Close()
			conn, buffer, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			defer conn.Close()
			fmt.Fprint(buffer, "HTTP/1.1 200 Connection Established\r\n\r\n")
			buffer.Flush()
			go func() { io.Copy(remote, buffer); remote.Close() }()
			io.Copy(conn, remote)
			return
		}
		page(w, r)
	})
	proxy := fixtureHTTPServer(proxyHandler, "PROXY", false)
	defer proxy.Close()
	tlsProxy := fixtureHTTPServer(proxyHandler, "TLS_PROXY", true)
	defer tlsProxy.Close()
	tlsSource := fixtureHTTPServer(http.HandlerFunc(page), "TLS_SOURCE", true)
	defer tlsSource.Close()
	// Trust only the local fixture certificates in this fixture process. The
	// production crawler still uses normal system certificate verification.
	roots := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsProxy.Certificate().Raw}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsSource.Certificate().Raw})...)
	rootFile := root + "/fixture-ca.pem"
	if err := os.WriteFile(rootFile, roots, 0600); err != nil {
		panic(err)
	}
	os.Setenv("SSL_CERT_FILE", rootFile)
	socks, err := startSOCKSFixture(ctx)
	if err != nil {
		panic(err)
	}
	defer socks.Close()
	address := os.Getenv("DELVER_WORKSPACE_FIXTURE_ADDR")
	if address == "" {
		address = "127.0.0.1:0"
	}
	options := application.Options{DataDirectory: root, Address: address}
	if os.Getenv("DELVER_WORKSPACE_FIXTURE_LEGACY") == "1" {
		registry := root + "/crawls.json"
		if _, e := os.Stat(registry); os.IsNotExist(e) {
			d := application.DefaultCrawlDefinition()
			d.Name = "Migrated crawl"
			d.Seeds = []string{sourceURL + "/site"}
			d.Fetch.Mode = "adaptive"
			d.Fetch.IncreaseStep = 2
			raw, _ := json.Marshal(d)
			var definition map[string]interface{}
			json.Unmarshal(raw, &definition)
			fetch := definition["fetch"].(map[string]interface{})
			delete(fetch, "adjustment_interval")
			fetch["evaluation_window"] = "45s"
			fetch["adjustment_cooldown"] = "60s"
			fetch["process_limit"] = 8
			fetch["process_origin_limit"] = 1
			fetch["process_origin_delay"] = "150ms"
			fetch["latency_inflation"] = 1.5
			raw, _ = json.Marshal(map[string]interface{}{"fixture-legacy": map[string]interface{}{"id": "fixture-legacy", "revision": 1, "definition": definition, "runs": []interface{}{map[string]interface{}{"id": "fixture-history", "version": 1, "state": "stopped", "desired": "stopped", "definition": definition, "summary": map[string]interface{}{"fetch": map[string]interface{}{"process_limit": 0}}}}}})
			if err := os.WriteFile(registry, raw, 0600); err != nil {
				panic(err)
			}
		}
	}
	workspace, err := application.NewWorkspace(ctx, options)
	if err != nil {
		panic(err)
	}
	defer workspace.Close()
	app, _ := application.DefaultApplication(options)
	app.Config.API.StaticFiles = ui.Assets()
	server, cleanup, err := api.NewServerWithOperations(app.Config.API, workspace)
	if err != nil {
		panic(err)
	}
	defer cleanup()
	mux := http.NewServeMux()
	mux.HandleFunc("/fixture/info", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"source": source.URL, "tls_source": tlsSource.URL, "https_proxy": tlsProxy.URL, "socks": "socks5://operator:browser-proxy-secret@" + socks.Addr().String(), "proxy": proxy.URL, "proxy_username": "operator", "proxy_password": "browser-proxy-secret"})
	})
	mux.HandleFunc("/fixture/socks-events", func(w http.ResponseWriter, r *http.Request) {
		socksEvents.Lock()
		defer socksEvents.Unlock()
		json.NewEncoder(w).Encode(socksEvents.Rows)
	})
	mux.HandleFunc("/fixture/network-activity", func(w http.ResponseWriter, r *http.Request) {
		networkActivity.Lock()
		defer networkActivity.Unlock()
		if r.Method == "POST" {
			networkActivity.Peak = networkActivity.Active
		}
		json.NewEncoder(w).Encode(map[string]int{"active": networkActivity.Active, "peak": networkActivity.Peak})
	})
	mux.HandleFunc("/fixture/request-events", func(w http.ResponseWriter, r *http.Request) {
		requestEvents.Lock()
		defer requestEvents.Unlock()
		rows := requestEvents.Rows
		if agent := r.URL.Query().Get("user_agent"); agent != "" {
			rows = nil
			for _, row := range requestEvents.Rows {
				if row["user_agent"] == agent {
					rows = append(rows, row)
				}
			}
		}
		json.NewEncoder(w).Encode(rows)
	})
	mux.HandleFunc("/fixture/pressure", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Token string `json:"token"`
			Fail  bool   `json:"fail"`
		}
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&p) != nil || p.Token == "" {
			http.Error(w, "invalid pressure control", 400)
			return
		}
		pressureFlag(p.Token).Store(p.Fail)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/fixture/recovery", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Token string `json:"token"`
			Fail  bool   `json:"fail"`
		}
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&p) != nil || p.Token == "" {
			http.Error(w, "invalid fixture control", 400)
			return
		}
		failureFlag(p.Token).Store(p.Fail)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/", server.Handler)
	server.Handler = mux
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		panic(err)
	}
	if state := os.Getenv("DELVER_WORKSPACE_FIXTURE_STATE_FILE"); state != "" {
		data, _ := json.Marshal(map[string]string{"url": "http://" + listener.Addr().String(), "storage": root})
		if err := os.WriteFile(state, data, 0600); err != nil {
			panic(err)
		}
	}
	go server.Serve(listener)
	<-ctx.Done()
	stop, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	server.Shutdown(stop)
}

func fixtureHTTPServer(handler http.Handler, key string, tls bool) *httptest.Server {
	server := httptest.NewUnstartedServer(handler)
	if address := os.Getenv("DELVER_WORKSPACE_FIXTURE_" + key + "_ADDR"); address != "" {
		server.Listener.Close()
		listener, err := net.Listen("tcp", address)
		if err != nil {
			panic(err)
		}
		server.Listener = listener
	}
	if tls {
		server.StartTLS()
	} else {
		server.Start()
	}
	return server
}
