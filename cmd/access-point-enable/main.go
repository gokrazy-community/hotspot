package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gokrazy-community/hotspot"
	"github.com/mdlayher/wifi"
	"golang.org/x/sys/unix"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

type (
	cidrFlag struct {
		IP  net.IP
		Net *net.IPNet
	}
)

// Set implements flag.Value.
func (c *cidrFlag) Set(s string) error {
	ip, subnet, err := net.ParseCIDR(s)
	if err != nil {
		return err
	}
	c.IP = ip
	c.Net = subnet
	return nil
}

// String implements flag.Value.
func (c *cidrFlag) String() string {
	if c == nil || c.IP == nil || c.Net == nil {
		return "<nil>"
	}
	bits, _ := c.Net.Mask.Size()
	return fmt.Sprintf("%s/%d", c.IP.String(), bits)
}

func run() error {
	var ifaceName string
	var ssid string
	var channel uint
	cidr := cidrFlag{
		IP: net.IP{172, 17, 2, 1},
		Net: &net.IPNet{
			IP: net.IP{172, 17, 2, 0},
			Mask: net.IPMask{
				255, 255, 255, 0,
			},
		},
	}
	flag.StringVar(&ifaceName, "iface", "", "name of the wifi interface (can be left empty if only one is available)")
	flag.Var(&cidr, "cidr", "CIDR to indicate the IP address of the wifi interface and the subnet to route via this interface")
	flag.StringVar(&ssid, "ssid", "gokrazy", "SSID of the wifi network")
	flag.UintVar(&channel, "channel", 0, "Channel of the wifi network (1-14, randomly chosen if unset)")
	flag.Parse()

	if channel == 0 {
		channel = 1 + rand.UintN(14)
	}
	radio := hotspot.Radio{
		SSID:           ssid,
		BeaconInterval: 100,
		Channel:        uint8(channel),
	}

	if err := loadDriver(); err != nil {
		return fmt.Errorf("loadDriver: %w", err)
	}

	iface, err := findWirelessInterface(ifaceName)
	if err != nil {
		return fmt.Errorf("findWirelessInterface: %w", err)
	}

	if err := hotspot.EnableAccessPointMode(hotspot.LinkConfig{
		Interface: iface,
		Addr: &net.IPNet{
			IP:   cidr.IP,
			Mask: net.IPMask{255, 255, 255, 255},
		},
		Route:          cidr.Net,
		BroadcastRoute: true,
	}, nil); err != nil {
		return fmt.Errorf("EnableAccessPointMode: %w", err)
	}

	if err := radio.StartBeacon(iface, nil); err != nil {
		return fmt.Errorf("StartBeacon: %w", err)
	}

	dm, err := newDHCP4Manager(
		cidr.IP, cidr.Net.Mask, cidr.IP,
	)
	if err != nil {
		return fmt.Errorf("dhcp config: %w", err)
	}
	dhcpServer, err := hotspot.NewDHCP4(iface.Name, dm)
	if err != nil {
		return fmt.Errorf("dhcp: %w", err)
	}
	defer dhcpServer.Close()
	log.Println("dhcp listening")
	return dhcpServer.Serve()
}

type lease struct {
	client net.HardwareAddr
	until  time.Time
}

func (l lease) suitable(hw net.HardwareAddr, now time.Time) bool {
	return now.Sub(l.until) > time.Second || bytes.Equal(l.client, hw)
}

type dhcpManager struct {
	Subnet  net.IPMask
	Routers []net.IP
	DNS     []net.IP

	reserved      map[uint32]struct{}
	firstIP       uint32 // inclusive
	lastIP        uint32 // exclusive
	leaseDuration time.Duration

	mu     sync.Mutex
	leases map[uint32]lease
}

func ip4ToUint(ip net.IP) uint32 {
	if len(ip) == 0 {
		return 0
	}
	return uint32(ip[3]) + 256*(uint32(ip[2])+256*(uint32(ip[1])+256*uint32(ip[0])))
}

func uintToIP4(u uint32) net.IP {
	return net.IP{
		byte((u >> 24) % 256),
		byte((u >> 16) % 256),
		byte((u >> 8) % 256),
		byte(u % 256),
	}
}

func newDHCP4Manager(router net.IP, mask net.IPMask, dns ...net.IP) (*dhcpManager, error) {
	routerIP := ip4ToUint(router.To4())
	if routerIP == 0 {
		return nil, fmt.Errorf("invalid router IP: %s", router)
	}

	dm := dhcpManager{
		Routers: []net.IP{router},
		Subnet:  mask,
		DNS:     dns,
		reserved: map[uint32]struct{}{
			routerIP: {},
		},
		leases: make(map[uint32]lease),
	}

	ones, bits := mask.Size()
	if bits != 32 || ones == bits {
		return nil, fmt.Errorf("invalid IPv4 mask: %s (%d bits, %d ones)", mask, bits, ones)
	}
	bitMask := (uint32(1) << (bits - ones)) - 1
	dm.firstIP = routerIP - (routerIP & bitMask) + 1
	dm.lastIP = dm.firstIP + bitMask
	if dm.firstIP == routerIP {
		dm.firstIP++
	}
	if !(dm.firstIP < dm.lastIP) {
		return nil, fmt.Errorf("empty range: %s", mask)
	}
	return &dm, nil
}

func (dm *dhcpManager) Discover(req hotspot.DHCPRequest) hotspot.DHCPReply {
	ip, d := dm.findIP(req.WishIP, req.HardwareAddr, false)
	log.Println("discover", req, ip)
	return hotspot.DHCPReply{
		IP:      ip,
		Lease:   d,
		Subnet:  dm.Subnet,
		Routers: dm.Routers,
		DNS:     dm.DNS,
	}
}

func (dm *dhcpManager) Request(req hotspot.DHCPRequest) hotspot.DHCPReply {
	ip, d := dm.findIP(req.WishIP, req.HardwareAddr, true)
	log.Println("request", req, ip)
	return hotspot.DHCPReply{
		IP:      ip,
		Lease:   d,
		Subnet:  dm.Subnet,
		Routers: dm.Routers,
		DNS:     dm.DNS,
	}
}

func (dm *dhcpManager) findIP(ip net.IP, hw net.HardwareAddr, allocate bool) (net.IP, time.Duration) {
	u := dm.suggestIP(ip, hw)

	dm.mu.Lock()
	defer dm.mu.Unlock()
	now := time.Now()
	l, taken := dm.leases[u]
	if taken && !l.suitable(hw, now) {
		// address already allocated to someone else
		taken = true
		lastU := u
		for ; u < dm.lastIP; u++ {
			l, taken = dm.leases[u]
			if !taken || l.suitable(hw, now) {
				taken = false
				break
			}
		}
		if taken {
			// search from the start
			for u = dm.firstIP; u < lastU; u++ {
				l, taken = dm.leases[u]
				if !taken || l.suitable(hw, now) {
					taken = false
					break
				}
			}
		}
		if taken {
			// nothing found
			return nil, 0
		}
	}

	// found a suitable address
	until := now.Add(dm.leaseDuration)
	if allocate {
		dm.leases[u] = lease{
			client: hw,
			until:  until,
		}
	}
	return uintToIP4(u), dm.leaseDuration
}

func (dm *dhcpManager) suggestIP(ip net.IP, hw net.HardwareAddr) uint32 {
	u := ip4ToUint(ip)
	if dm.firstIP <= u && u < dm.lastIP {
		return u
	}

	u = 0
	mod := dm.lastIP - dm.firstIP
	for _, b := range hw {
		u = (u*256 + uint32(b)) % mod
	}
	return dm.firstIP + u
}

func (dm *dhcpManager) Err(err error) {
	log.Println("dhcp error", err)
}

func findWirelessInterface(name string) (*net.Interface, error) {
	cl, err := wifi.New()
	if err != nil {
		return nil, err
	}
	defer cl.Close()

	interfaces, err := cl.Interfaces()
	if err != nil {
		return nil, err
	}
	if len(interfaces) == 0 {
		return nil, fmt.Errorf("no wifi interface found")
	}

	var iface *wifi.Interface
	if name == "" {
		if len(interfaces) > 1 {
			names := make([]string, 0, len(interfaces))
			for _, ifc := range interfaces {
				names = append(names, fmt.Sprintf("%q (%d)", ifc.Name, ifc.Index))
			}
			return nil, fmt.Errorf("multiple wifi interfaces available: %v", names)
		}
		iface = interfaces[0]

	} else {
		for _, ifc := range interfaces {
			if ifc.Name != name {
				continue
			}
			iface = ifc
			break
		}
		if iface == nil {
			names := make([]string, 0, len(interfaces))
			for _, ifc := range interfaces {
				names = append(names, fmt.Sprintf("%q (%d)", ifc.Name, ifc.Index))
			}
			return nil, fmt.Errorf("%q not found, available interfaces: %v", name, names)
		}
	}
	// somewhat hacky unfortunately
	return &net.Interface{
		Index: iface.Index,
		// MTU:          0,
		Name:         iface.Name,
		HardwareAddr: iface.HardwareAddr,
		// Flags:        0,
	}, nil
}

func loadDriver() error {
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		return fmt.Errorf("Uname: %w", err)
	}
	release := string(uts.Release[:bytes.IndexByte(uts.Release[:], 0)])

	// modprobe the brcmfmac driver
	for _, mod := range []string{
		"kernel/drivers/net/wireless/broadcom/brcm80211/brcmutil/brcmutil.ko",
		"kernel/drivers/net/wireless/broadcom/brcm80211/brcmfmac/brcmfmac.ko",
		"kernel/drivers/net/wireless/broadcom/brcm80211/brcmfmac/wcc/brcmfmac-wcc.ko",
	} {
		if err := loadModule(release, mod); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func loadModule(release, mod string) error {
	f, err := os.Open(filepath.Join("/lib/modules", release, mod))
	if err != nil {
		return err
	}
	defer f.Close()
	if err := unix.FinitModule(int(f.Fd()), "", 0); err != nil {
		if err != unix.EEXIST &&
			err != unix.EBUSY &&
			err != unix.ENODEV &&
			err != unix.ENOENT {
			return fmt.Errorf("FinitModule(%v): %v", mod, err)
		}
	}
	return nil
}
