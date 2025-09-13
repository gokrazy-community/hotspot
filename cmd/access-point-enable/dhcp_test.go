package main

import (
	"net"
	"testing"

	"code.pfad.fr/check"
)

func TestDHCP(t *testing.T) {
	m, err := newDHCP4Manager(net.IP{172, 17, 2, 1}, net.IPMask{255, 255, 255, 0})
	check.Equal(t, nil, err)
	routerIP := uint32(1 + 256*(2+256*(17+256*172)))
	check.EqualDeep(t, map[uint32]struct{}{
		routerIP: {},
	}, m.reserved)
	check.Equal(t, ip4ToUint(net.IP{172, 17, 2, 2}), m.firstIP)
	check.Equal(t, ip4ToUint(net.IP{172, 17, 2, 255})+1, m.lastIP)
	check.Equal(t, ip4ToUint(net.IP{172, 17, 2, 2}), m.suggestIP(
		net.IP{172, 17, 2, 2}, nil,
	))
	check.Equal(t, ip4ToUint(net.IP{172, 17, 2, 20}), m.suggestIP(
		net.IP{172, 17, 2, 20}, nil,
	))

	check.Equal(t, ip4ToUint(net.IP{172, 17, 2, 28}), m.suggestIP(
		nil, net.HardwareAddr{1, 2, 3, 4},
	))

	check.EqualDeep(t, net.IP{2, 3, 5, 7}, uintToIP4(ip4ToUint(net.IP{2, 3, 5, 7})))

	// allocation
	ip, _ := m.findIP(net.IP{172, 17, 2, 2}, net.HardwareAddr{1, 2, 3, 4}, true)
	check.EqualDeep(t, net.IP{172, 17, 2, 2}, ip).Log(ip.String())
	// re-allocate to same client
	ip, _ = m.findIP(net.IP{172, 17, 2, 2}, net.HardwareAddr{1, 2, 3, 4}, true)
	check.EqualDeep(t, net.IP{172, 17, 2, 2}, ip).Log(ip.String())
	// re-allocate to other client
	ip, _ = m.findIP(net.IP{172, 17, 2, 2}, net.HardwareAddr{42, 2, 3, 4}, true)
	check.EqualDeep(t, net.IP{172, 17, 2, 3}, ip).Log(ip.String())
	// re-allocate to other client
	ip, _ = m.findIP(net.IP{172, 17, 2, 2}, net.HardwareAddr{42, 2, 3, 4}, true)
	check.EqualDeep(t, net.IP{172, 17, 2, 3}, ip).Log(ip.String())

	// allocate to other client
	ip, _ = m.findIP(net.IP{172, 17, 2, 255}, net.HardwareAddr{100, 2, 3, 4}, true)
	check.EqualDeep(t, net.IP{172, 17, 2, 255}, ip).Log(ip.String())
	// re-allocate to other client
	ip, _ = m.findIP(net.IP{172, 17, 2, 255}, net.HardwareAddr{101, 2, 3, 4}, true)
	check.EqualDeep(t, net.IP{172, 17, 2, 4}, ip).Log(ip.String())

	t.Run("full", func(t *testing.T) {
		m, err := newDHCP4Manager(net.IP{172, 17, 2, 1}, net.IPMask{255, 255, 255, 252})
		check.Equal(t, nil, err).Fatal()
		ip, _ := m.findIP(net.IP{172, 17, 2, 2}, net.HardwareAddr{1, 2, 3, 4}, true)
		check.EqualDeep(t, net.IP{172, 17, 2, 2}, ip).Log(ip.String())
		ip, _ = m.findIP(net.IP{172, 17, 2, 2}, net.HardwareAddr{42, 2, 3, 4}, true)
		check.EqualDeep(t, net.IP{172, 17, 2, 3}, ip).Log(ip.String())
		ip, _ = m.findIP(net.IP{172, 17, 2, 3}, net.HardwareAddr{33, 2, 3, 4}, true)
		check.EqualDeep(t, nil, ip).Log(ip.String())
	})
}
