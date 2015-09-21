/*
gdns is a dns proxy server write by go.

gdns much like dnsmasq or chinadns, but it can run on windows.

Features:

    support different domains use different upstream dns servers
    support contact to the upstream dns server by tcp or udp
    support blacklist list to block the fake ip

Usage:

generate a config file and edit it
    $ gdns -dumpflags > dns.ini


run it
    $ sudo gdns -config dns.ini


*/
package main

import (
	"github.com/miekg/dns"
	"log"
)

var client_udp *dns.Client = &dns.Client{}

var client_tcp *dns.Client = &dns.Client{Net: "tcp"}

var Servers []*UpstreamServer = nil

var logger *LogOut = nil

var Blacklist_ips Kv = nil

var debug bool = false

var hostfile string = ""
var record_hosts Hosts = nil

func in_blacklist(m *dns.Msg) bool {
	if Blacklist_ips == nil {
		return false
	}

	if m == nil {
		return false
	}

	for _, rr := range m.Answer {
		if t, ok := rr.(*dns.A); ok {
			ip := t.A.String()
			if _, ok1 := Blacklist_ips[ip]; ok1 {
				logger.Debug("%s is in blacklist\n", ip)
				return true
			}
		}
	}
	return false
}
func handleRoot(w dns.ResponseWriter, r *dns.Msg) {
	var err error
	var res *dns.Msg
	domain := r.Question[0].Name

	var done int

	/*
	   reply from hosts
	*/
	if record_hosts != nil {
		rr := record_hosts.Get(domain, r.Question[0].Qtype)
		if rr != nil {
			msg := new(dns.Msg)
			msg.SetReply(r)
			msg.Answer = append(msg.Answer, rr)
			w.WriteMsg(msg)
			logger.Debug("query %s %s %s, reply from hosts\n",
				domain,
				dns.ClassToString[r.Question[0].Qclass],
				dns.TypeToString[r.Question[0].Qtype],
			)
			return
		}
	}

	for i := 0; i < 2; i++ {
		done = 0
		for _, sv := range Servers {
			if sv.match(domain) {
				logger.Debug("query %s %s %s, forward to %s:%s\n",
					domain,
					dns.ClassToString[r.Question[0].Qclass],
					dns.TypeToString[r.Question[0].Qtype],
					sv.Proto, sv.Addr)
				res, err = sv.query(r)
				if err == nil {
					if !in_blacklist(res) && res.Rcode != dns.RcodeServerFailure {
						w.WriteMsg(res)
						done = 1
						break
					}
				} else {
					logger.Error("%s", err)
				}
			}
		}

		if done != 1 {
			res, err = DefaultServer.query(r)
			logger.Debug("query %s %s %s, use default server %s:%s\n",
				domain,
				dns.ClassToString[r.Question[0].Qclass],
				dns.TypeToString[r.Question[0].Qtype],
				DefaultServer.Proto, DefaultServer.Addr)
			if err == nil {
				if !in_blacklist(res) && res.Rcode != dns.RcodeServerFailure {
					w.WriteMsg(res)
					done = 1
				}
			} else {
				logger.Error("%s", err)
			}
		}

		if done == 1 {
			break
		}
	}

	if done != 1 {
		dns.HandleFailed(w, r)
	}
}

func main() {
	parse_flags()

	dns.HandleFunc(".", handleRoot)

	logger = NewLogger(logfile, debug)

	logger.Info("Listen on %s\n", bind_addr)

	err := dns.ListenAndServe(bind_addr, "udp", nil)
	if err != nil {
		log.Fatal(err)
	}
}
