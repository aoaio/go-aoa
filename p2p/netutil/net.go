package netutil

import (
	"errors"
	"net"
	"strings"
)

var lan4, lan6, special4, special6 Netlist

func init() {

	lan4.Add("0.0.0.0/8")
	lan4.Add("10.0.0.0/8")
	lan4.Add("172.16.0.0/12")
	lan4.Add("192.168.0.0/16")
	lan6.Add("fe80::/10")
	lan6.Add("fc00::/7")
	special4.Add("192.0.0.0/29")
	special4.Add("192.0.0.9/32")
	special4.Add("192.0.0.170/32")
	special4.Add("192.0.0.171/32")
	special4.Add("192.0.2.0/24")
	special4.Add("192.31.196.0/24")
	special4.Add("192.52.193.0/24")
	special4.Add("192.88.99.0/24")
	special4.Add("192.175.48.0/24")
	special4.Add("198.18.0.0/15")
	special4.Add("198.51.100.0/24")
	special4.Add("203.0.113.0/24")
	special4.Add("255.255.255.255/32")

	special6.Add("100::/64")
	special6.Add("2001::/32")
	special6.Add("2001:1::1/128")
	special6.Add("2001:2::/48")
	special6.Add("2001:3::/32")
	special6.Add("2001:4:112::/48")
	special6.Add("2001:5::/32")
	special6.Add("2001:10::/28")
	special6.Add("2001:20::/28")
	special6.Add("2001:db8::/32")
	special6.Add("2002::/16")
}

type Netlist []net.IPNet

func ParseNetlist(s string) (*Netlist, error) {
	ws := strings.NewReplacer(" ", "", "\n", "", "\t", "")
	masks := strings.Split(ws.Replace(s), ",")
	l := make(Netlist, 0)
	for _, mask := range masks {
		if mask == "" {
			continue
		}
		_, n, err := net.ParseCIDR(mask)
		if err != nil {
			return nil, err
		}
		l = append(l, *n)
	}
	return &l, nil
}

func (l Netlist) MarshalTOML() interface{} {
	list := make([]string, 0, len(l))
	for _, net := range l {
		list = append(list, net.String())
	}
	return list
}

func (l *Netlist) UnmarshalTOML(fn func(interface{}) error) error {
	var masks []string
	if err := fn(&masks); err != nil {
		return err
	}
	for _, mask := range masks {
		_, n, err := net.ParseCIDR(mask)
		if err != nil {
			return err
		}
		*l = append(*l, *n)
	}
	return nil
}

func (l *Netlist) Add(cidr string) {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	*l = append(*l, *n)
}

func (l *Netlist) Contains(ip net.IP) bool {
	if l == nil {
		return false
	}
	for _, net := range *l {
		if net.Contains(ip) {
			return true
		}
	}
	return false
}

func IsLAN(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		return lan4.Contains(v4)
	}
	return lan6.Contains(ip)
}

func IsSpecialNetwork(ip net.IP) bool {
	if ip.IsMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		return special4.Contains(v4)
	}
	return special6.Contains(ip)
}

var (
	errInvalid     = errors.New("invalid IP")
	errUnspecified = errors.New("zero address")
	errSpecial     = errors.New("special network")
	errLoopback    = errors.New("loopback address from non-loopback host")
	errLAN         = errors.New("LAN address from WAN host")
)

func CheckRelayIP(sender, addr net.IP) error {
	if len(addr) != net.IPv4len && len(addr) != net.IPv6len {
		return errInvalid
	}
	if addr.IsUnspecified() {
		return errUnspecified
	}
	if IsSpecialNetwork(addr) {
		return errSpecial
	}
	if addr.IsLoopback() && !sender.IsLoopback() {
		return errLoopback
	}
	if IsLAN(addr) && !IsLAN(sender) {
		return errLAN
	}
	return nil
}
