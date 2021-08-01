package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const (
	Attempts int = iota
	Retry
)

func healthCheck() {
	t := time.NewTicker(time.Minute * 2)
	for {
		select {
		case <-t.C:
			log.Println("Starting health check...")
			serverPool.HealthCheck()
			log.Println("Health check complated")
		}
	}
}

func isInstanceAlive(u *url.URL) bool {
	timeout := 2 * time.Second
	conn, err := net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		log.Println("Site unreachable, err: ", err)
		return false
	}
	_ = conn.Close()
	return true
}

func GetAttemptsFromContext(r *http.Request) int {
	if attempts, ok := r.Context().Value(Attempts).(int); ok {
		return attempts
	}
	return 1
}

func GetRetryFromContext(r *http.Request) int {
	if retry, ok := r.Context().Value(Retry).(int); ok {
		return retry
	}
	return 0
}

func loadBalancer(w http.ResponseWriter, r *http.Request) {
	instance := serverPool.NextServer()
	if instance != nil {
		instance.ReverseProxy.ServeHTTP(w, r)
		return
	}
	http.Error(w, "Server not available", http.StatusServiceUnavailable)
}

var serverPool ServerPool

func main() {
	var instanceList string
	var port int
	flag.StringVar(&instanceList, "servers", "", "Load balanced servers, use commas to separate")
	flag.IntVar(&port, "port", 3030, "Port to serve")
	flag.Parse()

	if len(instanceList) == 0 {
		log.Fatal("Must specify at least one instance")
		panic(-1)
	}

	tokens := strings.Split(instanceList, ",")
	for _, tok := range tokens {
		serverUrl, err := url.Parse(tok)
		if err != nil {
			log.Fatal("Error parsing server URL", err)
		}

		proxy := httputil.NewSingleHostReverseProxy(serverUrl)
		proxy.ErrorHandler = func(writer http.ResponseWriter, req *http.Request, e error) {
			log.Printf("[%s] %s\n", serverUrl.Host, e.Error())
			retries := GetRetryFromContext(req)
			if retries < 3 {
				select {
				case <-time.After(10 * time.Millisecond):
					ctx := context.WithValue(req.Context(), Retry, retries+1)
					proxy.ServeHTTP(writer, req.WithContext(ctx))
				}
			}

			serverPool.SetServerStatus(serverUrl, false)

			attempts := GetAttemptsFromContext(req)
			log.Printf("%s(%s) Attempting retry %d\n", req.RemoteAddr, req.URL.Path, attempts)
			ctx := context.WithValue(req.Context(), Attempts, attempts+1)
			loadBalancer(writer, req.WithContext(ctx))
		}

		serverPool.AddServer(&Server{
			URL:          serverUrl,
			Alive:        true,
			ReverseProxy: proxy,
		})
		log.Printf("Configured instance: %s\n", serverUrl)
	}

	server := http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: http.HandlerFunc(loadBalancer),
	}

	go healthCheck()

	log.Printf("Load Balancer started at :%d\n", port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}

}
