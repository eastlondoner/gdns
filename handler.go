package gdns

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fangdingjun/go-log"
	"github.com/miekg/dns"
)

type dnsClient interface {
	Exchange(m *dns.Msg, addr string) (*dns.Msg, time.Duration, error)
}

type DnsHandler struct {
	cfg         *Conf
	tcpclient   dnsClient
	udpclient   dnsClient
	httpsgoogle dnsClient
	httpscf     dnsClient
}

func NewDNSHandler(cfg *Conf) dns.Handler {
	return &DnsHandler{
		cfg:         cfg,
		tcpclient:   &dns.Client{Net: "tcp", Timeout: 8 * time.Second, UDPSize: 4096},
		udpclient:   &dns.Client{Net: "udp", Timeout: 5 * time.Second, UDPSize: 4096},
		httpsgoogle: &GoogleHTTPDns{},
		httpscf:     &CloudflareHTTPDns{},
	}

}

// ServerDNS implements the dns.Handler interface
func (h *DnsHandler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	domain := r.Question[0].Name


	if ok := h.answerFromHosts(w, r); ok {
		log.Infof("Found in hosts")
		return
	}

	srvs := h.getUpstreamServer(domain)
	if srvs == nil {
		srvs = h.cfg.DefaultUpstream
	}

	if msg, err := h.getAnswerFromUpstream(r, srvs); err == nil {
		log.Infof("Found in upstream")
		w.WriteMsg(msg)
		return
	}
	log.Infof("Failed")

	dns.HandleFailed(w, r)

}

func (h *DnsHandler) getUpstreamServer(domain string) []Addr {
	for _, srv := range h.cfg.ForwardRules {
		if ok := srv.domains.has(strings.Trim(domain, ".")); ok {
			return srv.Server
		}
	}
	return nil
}

func (h *DnsHandler) queryUpstream(r *dns.Msg, srv Addr, ch chan *dns.Msg) {
	var m *dns.Msg
	var err error

	switch srv.Network {
	case "tcp":
		log.Debugf("query %s IN %s, forward to %s:%d through tcp",
			r.Question[0].Name,
			dns.TypeToString[r.Question[0].Qtype],
			srv.Host,
			srv.Port)
		m, _, err = h.tcpclient.Exchange(r, fmt.Sprintf("%s:%d", srv.Host, srv.Port))
	case "udp":
		log.Debugf("query %s IN %s, forward to %s:%d through udp",
			r.Question[0].Name,
			dns.TypeToString[r.Question[0].Qtype],
			srv.Host,
			srv.Port)
		m, _, err = h.udpclient.Exchange(r, fmt.Sprintf("%s:%d", srv.Host, srv.Port))
	case "https_google":
		log.Debugf("query %s IN %s, forward to %s:%d through google https",
			r.Question[0].Name,
			dns.TypeToString[r.Question[0].Qtype],
			srv.Host,
			srv.Port)
		m, _, err = h.httpsgoogle.Exchange(r, fmt.Sprintf("%s:%d", srv.Host, srv.Port))
	case "https_cloudflare":
		log.Debugf("query %s IN %s, forward to %s:%d through cloudflare https",
			r.Question[0].Name,
			dns.TypeToString[r.Question[0].Qtype],
			srv.Host,
			srv.Port)
		m, _, err = h.httpscf.Exchange(r, srv.Host)
	default:
		err = fmt.Errorf("not supported type %s", srv.Network)
	}

	if err != nil {
		log.Errorf("%s", err)
		return
	}

	select {
	case ch <- m:
	default:
	}
}

func (h *DnsHandler) getAnswerFromUpstream(r *dns.Msg, servers []Addr) (*dns.Msg, error) {
	ch := make(chan *dns.Msg, len(servers))
	for _, srv := range servers {
		go func(a Addr) {
			h.queryUpstream(r, a, ch)
		}(srv)
	}

	var savedErr *dns.Msg
	for {
		select {
		case m := <-ch:
			if m.Rcode == dns.RcodeSuccess && !h.inBlacklist(m) {
				return m, nil
			}
			savedErr = m
		case <-time.After(time.Duration(h.cfg.Timeout) * time.Second):
			if savedErr != nil {
				return savedErr, nil
			}
			log.Debugf("query %s IN %s, timeout", r.Question[0].Name, dns.TypeToString[r.Question[0].Qtype])
			return nil, errors.New("timeout")
		}
	}
}

func (h *DnsHandler) inBlacklist(m *dns.Msg) bool {
	var ip string
	for _, rr := range m.Answer {
		if a, ok := rr.(*dns.A); ok {
			ip = a.A.String()
		} else if aaaa, ok := rr.(*dns.AAAA); ok {
			ip = aaaa.AAAA.String()
		} else {
			ip = ""
		}
		if ip != "" && h.cfg.blacklist.exists(ip) {
			log.Debugf("%s in blacklist", ip)
			return true
		}
	}
	return false
}

func (h *DnsHandler) answerFromHosts(w dns.ResponseWriter, r *dns.Msg) bool {
	domain := r.Question[0].Name
	domain = strings.Trim(domain, ".") // Remove weird initial / trailing dots
	t := r.Question[0].Qtype

	log.Infof("request for %s %s", domain, t)

	ip := h.cfg.Hosts.get(domain, int(t))
	if ip != "" {
		rr, _ := dns.NewRR(fmt.Sprintf("%s 3600 IN %s %s", domain, dns.TypeToString[t], ip))
		if rr == nil {
			return false
		}
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.Answer = append(msg.Answer, rr)
		w.WriteMsg(msg)
		log.Debugf("query %s IN %s, reply from Hosts", domain, dns.TypeToString[t])
		return true
	}
	return false
}
