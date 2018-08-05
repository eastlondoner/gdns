package gdns

import (
	"bufio"
	"io/ioutil"
	"os"
	"strings"

	"github.com/go-yaml/yaml"
	"sync"
	"github.com/fangdingjun/go-log"
)

type Conf struct {
	Listen          []Addr
	BlacklistFile   string
	HostFile        string
	ForwardRules    []Rule
	DefaultUpstream []Addr
	Timeout         int
	Debug           bool
	blacklist       item
	Hosts           *Hostitem
}

type Rule struct {
	Server     []Addr
	DomainFile string
	domains    item
}

type Addr struct {
	Host    string
	Port    int
	Network string
}

func loadConfig(f string) (*Conf, error) {
	c := new(Conf)
	data, err := ioutil.ReadFile(f)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, err
	}

	if c.blacklist == nil {
		c.blacklist = item{}
	}

	if c.Timeout == 0 {
		c.Timeout = 2
	}

	if err := loadItemFile(c.blacklist, c.BlacklistFile); err != nil {
		return nil, err
	}

	for i := range c.ForwardRules {
		if c.ForwardRules[i].domains == nil {
			c.ForwardRules[i].domains = item{}
		}
		if err := loadItemFile(c.ForwardRules[i].domains,
			c.ForwardRules[i].DomainFile); err != nil {
			return nil, err
		}
	}

	if c.Hosts == nil {
		c.Hosts = new(Hostitem)
	}

	if err := loadHostsFile(c.Hosts, c.HostFile); err != nil {
		return nil, err
	}

	return c, nil
}

func loadHostsFile(h *Hostitem, f string) error {

	if f == "" {
		return nil
	}
	fd, err := os.Open(f)
	if err != nil {
		return err
	}
	defer fd.Close()

	r := bufio.NewReader(fd)
	for {
		s, err := r.ReadString('\n')
		if err != nil {
			break
		}
		s1 := strings.Trim(s, " \t\r\n")

		// ignore blank line and comment
		if s1 == "" || s1[0] == '#' {
			continue
		}
		s1 = strings.Replace(s1, "\t", " ", -1)
		s1 = strings.Trim(s1, " \t\r\n")
		ss := strings.Split(s1, " ")

		// ipv4
		t := 1
		if strings.Index(ss[0], ":") != -1 {
			// ipv6
			t = 28
		}

		for _, s2 := range ss[1:] {
			if s2 == "" {
				continue
			}

			h.Add(s2, ss[0], t)
		}

	}
	return nil
}

func loadItemFile(it item, f string) error {
	if f == "" {
		return nil
	}
	fd, err := os.Open(f)
	if err != nil {
		return err
	}
	defer fd.Close()

	r := bufio.NewReader(fd)
	for {
		s, err := r.ReadString('\n')
		if s != "" {
			s1 := strings.Trim(s, " \r\n")
			if s1 != "" && s1[0] != '#' {
				it.add(s1)
			}
		}
		if err != nil {
			break
		}
	}
	return nil
}

type item map[string]int

func (it item) has(s string) bool {
	ss := strings.Split(s, ".")

	for i := 0; i < len(ss); i++ {
		s1 := strings.Join(ss[i:], ".")
		if _, ok := it[s1]; ok {
			return true
		}
	}
	return false
}

func (it item) exists(s string) bool {
	_, ok := it[s]
	return ok
}

func (it item) add(s string) {
	it[s] = 1
}

type Hostitem struct {
	hosts map[string][]Hostentry
	lock  sync.Mutex
}


func (ht *Hostitem) get(domain string, t int) string {
	if v, ok := ht.hosts[domain]; ok {
		for _, v1 := range v {
			if v1.Domain == domain && v1.T == t {
				return v1.IP
			}
		}
	}
	// Look for matching wildcard hosts
	for host, v := range ht.hosts {
		if strings.HasPrefix(host, "*") && strings.HasSuffix(domain, strings.TrimPrefix(host, "*")) {
			for _, v1 := range v {
				if v1.Domain == domain && v1.T == t {
					return v1.IP
				}
			}
		}
	}
	return ""
}

func (ht *Hostitem) Add(domain, ip string, t int) {
	ht.lock.Lock()
	defer ht.lock.Unlock()

	if ht.hosts == nil {
		ht.hosts = make(map[string][]Hostentry)
	}

	if v, ok := ht.hosts[domain]; ok {
		exists := false
		for _, v1 := range v {
			if v1.Domain == domain && v1.IP == ip && v1.T == t {
				exists = true
				break
			}
		}
		if !exists {
			ht.hosts[domain] = append(ht.hosts[domain], Hostentry{domain, ip, t})
		}
	} else {
		v1 := []Hostentry{{domain, ip, t}}
		ht.hosts[domain] = v1
	}
}

func (ht *Hostitem) Remove(domain, ip string, t int) {
	ht.lock.Lock()
	defer ht.lock.Unlock()

	if ht.hosts == nil {
		ht.hosts = make(map[string][]Hostentry)
	}

	if v, ok := ht.hosts[domain]; ok {

		for i := len(v) - 1; i >= 0; i-- {
			v1 := v[i]
			if v1.Domain == domain && v1.IP == ip && v1.T == t {
				v = append(v[:i], v[i+1:]...)
			}
		}
		if len(v) == 0 {
			delete(ht.hosts, domain)
		} else {
			ht.hosts[domain] = v
		}
	} else {
		log.Warnf("Attempted to remove item %s %s %d which is not present", domain, ip, t)
		// TODO: do more than log?
	}
}

type Hostentry struct {
	Domain string
	IP     string
	T      int
}
