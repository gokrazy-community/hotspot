package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"

	"codeberg.org/miekg/dns"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

type (
	ipFlag struct {
		IP net.IP
	}
)

// Set implements flag.Value.
func (c *ipFlag) Set(s string) error {
	ip := net.ParseIP(s).To4()
	if ip == nil {
		return fmt.Errorf("unparseable IPv4: %q", s)
	}
	c.IP = ip
	return nil
}

// String implements flag.Value.
func (c *ipFlag) String() string {
	return c.IP.String()
}

func run() error {
	aRR := ipFlag{
		IP: net.IP{172, 17, 2, 1},
	}
	var addr string
	var network string
	flag.StringVar(&addr, "addr", ":domain", "addr to listen bind to")
	flag.StringVar(&network, "network", "udp", "network to bind to")
	flag.Var(&aRR, "a", "IPv4 to reply for all A questions")
	flag.Parse()

	aValid := !aRR.IP.IsUnspecified()

	server := dns.NewServer()
	server.Net = network
	server.Addr = addr
	server.Handler = dns.HandlerFunc(func(ctx context.Context, w dns.ResponseWriter, m *dns.Msg) {
		rep := new(dns.Msg)
		rep.ID = m.ID
		rep.Response = true
		for _, q := range m.Question {
			rep.Question = append(rep.Question, q)

			resp := "-"
			switch q.(type) {
			case *dns.A:
				if aValid {
					rep.Answer = append(rep.Answer, &dns.A{
						Hdr: dns.Header{
							Name:  q.Header().Name,
							Class: q.Header().Class,
							TTL:   60,
						},
						A: aRR.IP,
					})
					resp = aRR.IP.String()
				}
			}

			log.Printf("%s %s %s\n", resp, rrName(q), q.Header().Name)
		}

		err := rep.Pack()
		if err != nil {
			log.Println("pack", err)
		}
		_, err = io.Copy(w, rep)
		if err != nil {
			log.Println("copy", err)
		}
	})
	log.Println("dns serving on", addr)
	return server.ListenAndServe()
}

func rrName(rr dns.RR) string {
	t := dns.RRToType(rr)
	if s, ok := dns.TypeToString[t]; ok {
		return s
	}
	return "TYPE" + strconv.FormatUint(uint64(t), 10)
}
