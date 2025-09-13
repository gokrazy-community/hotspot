package hotspot

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"

	"github.com/gokrazy-community/hotspot/nl80211"
	"github.com/jsimonetti/rtnetlink/v2/rtnl"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

type LinkConfig struct {
	Interface      *net.Interface
	Addr           *net.IPNet
	Route          *net.IPNet
	BroadcastRoute bool
}

func EnableAccessPointMode(cfg LinkConfig, netcfg *netlink.Config) error {
	nlConn, err := nl80211.Dial(netcfg)
	if err != nil {
		return fmt.Errorf("Dial(genetlink): %w", err)
	}
	defer nlConn.Close()

	rtConn, err := rtnl.Dial(netcfg)
	if err != nil {
		return fmt.Errorf("Dial(rtnl): %w", err)
	}
	defer rtConn.Close()

	if err := rtConn.LinkDown(cfg.Interface); err != nil {
		return fmt.Errorf("Link(Down): %w", err)
	}

	if err := nlConn.SetInterface(cfg.Interface, unix.NL80211_IFTYPE_AP); err != nil {
		return err
	}

	if err := rtConn.LinkUp(cfg.Interface); err != nil {
		return fmt.Errorf("Link(Up): %w", err)
	}

	if cfg.Addr != nil {
		if err := rtConn.AddrAdd(cfg.Interface, cfg.Addr); err != nil && !errors.Is(err, fs.ErrExist) {
			log.Printf("%#v\n", err)
			return fmt.Errorf("AddrAdd: %w", err)
		}
	}
	if cfg.Route != nil {
		if err := rtConn.RouteAdd(cfg.Interface, *cfg.Route, nil); err != nil && !errors.Is(err, fs.ErrExist) {
			log.Printf("%#v\n", err)
			return fmt.Errorf("RouteAdd: %w", err)
		}
	}
	if cfg.BroadcastRoute {
		if err := rtConn.RouteAdd(cfg.Interface, net.IPNet{
			IP:   net.IP{255, 255, 255, 255},
			Mask: net.IPMask{255, 255, 255, 255},
		}, nil); err != nil {
			return fmt.Errorf("RouteAddBroadcast: %w", err)
		}
	}
	// route, err := rtConn.RouteGet(net.IP{255, 255, 255, 255})
	// log.Println("route", route, route.Destination, route.Interface.Name, err)
	// route, err = rtConn.RouteGet(net.IP{172, 17, 2, 67})
	// if err != nil {
	// 	log.Println("route 172.17.2.67", err)
	// } else {
	// 	log.Println("route", route, route.Destination, route.Interface.Name, err)
	// }
	// route, err = rtConn.RouteGet(cfg.Addr.IP)
	// log.Println("route", route, route.Destination, route.Interface.Name, err)

	return nil
}
