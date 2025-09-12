package nl80211

// Inspired by
// https://github.com/mdlayher/wifi/blob/v0.6.0/client_linux.go
// (MIT License)

import (
	"fmt"
	"net"

	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

type Conn struct {
	conn          *genetlink.Conn
	familyID      uint16
	familyVersion uint8
}

func Dial(cfg *netlink.Config) (*Conn, error) {
	conn, err := genetlink.Dial(cfg)
	if err != nil {
		return nil, err
	}

	// Make a best effort to apply the strict options set to provide better
	// errors and validation.
	for _, o := range []netlink.ConnOption{
		netlink.ExtendedAcknowledge,
		netlink.GetStrictCheck,
	} {
		_ = conn.SetOption(o, true)
	}

	family, err := conn.GetFamily(unix.NL80211_GENL_NAME)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	return &Conn{
		conn:          conn,
		familyID:      family.ID,
		familyVersion: family.Version,
	}, nil
}

func (conn *Conn) SetInterface(ifc *net.Interface, mode uint32) error {
	ae := netlink.NewAttributeEncoder()
	ae.Uint32(unix.NL80211_ATTR_IFINDEX, uint32(ifc.Index))
	ae.Uint32(unix.NL80211_ATTR_IFTYPE, mode)
	if err := conn.request(unix.NL80211_CMD_SET_INTERFACE, ae, netlink.Acknowledge); err != nil {
		return fmt.Errorf("SetInterface(nl80211): %w", err)
	}
	return nil
}

func (conn *Conn) request(cmd uint8, ae *netlink.AttributeEncoder, flags netlink.HeaderFlags) error {
	b, err := ae.Encode()
	if err != nil {
		return fmt.Errorf("netlink.AttributeEncode: %w", err)
	}
	_, err = conn.conn.Execute(
		genetlink.Message{
			Header: genetlink.Header{
				Command: cmd,
				Version: conn.familyVersion,
			},
			Data: b,
		},
		conn.familyID,
		netlink.Request|flags,
	)
	return err
}

func (c *Conn) Close() error {
	return c.conn.Close()
}
