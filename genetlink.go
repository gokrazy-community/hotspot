package hotspot

import (
	"fmt"

	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

type GenetlinkControl struct {
	Conn   *genetlink.Conn
	Family genetlink.Family
}

func NewGenericNetlinkControl(config *netlink.Config) (GenetlinkControl, error) {
	conn, err := genetlink.Dial(config)
	if err != nil {
		return GenetlinkControl{}, fmt.Errorf("genetlink.Dial: %w", err)
	}
	family, err := conn.GetFamily(unix.NL80211_GENL_NAME)
	if err != nil {
		return GenetlinkControl{}, fmt.Errorf("GetFamily: %v", err)
	}
	return GenetlinkControl{
		Conn:   conn,
		Family: family,
	}, nil
}

func (gc GenetlinkControl) Request(cmd uint8, ae *netlink.AttributeEncoder, flags netlink.HeaderFlags) error {
	b, err := ae.Encode()
	if err != nil {
		return fmt.Errorf("netlink.AttributeEncode: %w", err)
	}
	_, err = gc.Conn.Execute(
		genetlink.Message{
			Header: genetlink.Header{
				Command: cmd,
				Version: gc.Family.Version,
			},
			Data: b,
		},
		gc.Family.ID,
		netlink.Request|flags,
	)
	return err
}

func (gc GenetlinkControl) Close() error {
	return gc.Conn.Close()
}
