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

	dhcpServer, err := hotspot.NewDHCP4(iface.Name, &dhcpManager{})
	if err != nil {
		return fmt.Errorf("dhcp: %w", err)
	}
	defer dhcpServer.Close()
	log.Println("dhcp listening")
	return dhcpServer.Serve()
}

type dhcpManager struct{}

// Discover implements hotspot.DHCPHandler.
func (d *dhcpManager) Discover(req hotspot.DHCPRequest) hotspot.DHCPReply {
	log.Println("discover", req)
	return hotspot.DHCPReply{}
}

// Err implements hotspot.DHCPHandler.
func (d *dhcpManager) Err(err error) {
	log.Println("dhcp", err)
}

// Request implements hotspot.DHCPHandler.
func (d *dhcpManager) Request(req hotspot.DHCPRequest) hotspot.DHCPReply {
	log.Println("request", req)
	return hotspot.DHCPReply{}
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
