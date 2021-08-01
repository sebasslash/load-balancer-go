package main

import (
	"log"
	"net/url"
	"sync/atomic"
)

type ServerPool struct {
	servers []*Server
	current uint64
}

func (s *ServerPool) NextIndex() int {
	return int(atomic.AddUint64(&s.current, uint64(1)) % uint64(len(s.servers)))
}

func (s *ServerPool) AddServer(server *Server) {
	s.servers = append(s.servers, server)
}

func (s *ServerPool) NextServer() *Server {
	index := s.NextIndex()
	l := len(s.servers) + index
	for i := index; i < l; i++ {
		next := i % len(s.servers)

		if s.servers[next].IsAlive() {
			if i != index {
				atomic.StoreUint64(&s.current, uint64(next))
			}
			return s.servers[next]
		}
	}
	return nil
}

func (s *ServerPool) SetServerStatus(url *url.URL, alive bool) {
	for _, b := range s.servers {
		if b.URL.String() == url.String() {
			b.SetAlive(alive)
			break
		}
	}
}

func (s *ServerPool) HealthCheck() {
	for _, i := range s.servers {
		status := "up"
		alive := isInstanceAlive(i.URL)
		if !alive {
			status = "down"
		}

		i.SetAlive(alive)
		log.Printf("%s [%s]\n", i.URL, status)
	}
}
