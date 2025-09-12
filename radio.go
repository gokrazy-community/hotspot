package hotspot

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

type Radio struct {
	SSID           string
	BeaconInterval uint16
	Channel        uint8
}

func (r Radio) StartBeacon(iface *net.Interface, cfg *netlink.Config) error {
	if r.Channel < 1 || r.Channel > 14 {
		return fmt.Errorf("invalid channel %d (should be in range 1-14)", r.Channel)
	}

	beaconHead, err := r.beaconHead()
	if err != nil {
		return fmt.Errorf("beaconHead: %w", err)
	}

	gc, err := NewGenericNetlinkControl(cfg)
	if err != nil {
		return fmt.Errorf("Dial(genetlink): %w", err)
	}
	defer gc.Close()

	ae := netlink.NewAttributeEncoder()
	ae.Uint32(unix.NL80211_ATTR_IFINDEX, uint32(iface.Index))
	ae.Uint32(unix.NL80211_ATTR_BEACON_INTERVAL, uint32(r.BeaconInterval))
	ae.Uint32(unix.NL80211_ATTR_DTIM_PERIOD, 2)
	ae.Bytes(unix.NL80211_ATTR_BEACON_HEAD, beaconHead)
	ae.Bytes(unix.NL80211_ATTR_SSID, []byte(r.SSID))
	ae.Uint32(unix.NL80211_ATTR_WIPHY_FREQ, r.Frequency())
	ae.Uint32(unix.NL80211_ATTR_AUTH_TYPE, unix.NL80211_AUTHTYPE_OPEN_SYSTEM) // TODO: support some auth
	if err := gc.Request(unix.NL80211_CMD_START_AP, ae, netlink.Acknowledge); err != nil {
		return fmt.Errorf("Request(StartAP): %w", err)
	}
	return nil
}

func (r Radio) Frequency() uint32 {
	return 5*uint32(r.Channel) + 2407
}

// Standard 802.11 Information Element (IE) IDs
const (
	IE_SSID                = 0
	IE_SUPPORTED_RATES     = 1
	IE_DS_PARAM_SET        = 3
	IE_COUNTRY             = 7
	IE_ERP_INFO            = 42
	IE_EXT_SUPPORTED_RATES = 50
)

func (r Radio) beaconHead() ([]byte, error) {
	var buf bytes.Buffer
	// https://howiwifi.com/2020/07/13/802-11-frame-types-and-formats/

	// mac header
	buf.Write([]byte{
		0x80, 0x00, // frame control
		0x00, 0x00, // duration
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // DA
		0xdc, 0x97, 0xba, 0xb6, 0xdf, 0xa5, // SA
		0xdc, 0x97, 0xba, 0xb6, 0xdf, 0xa5, // BSS ID
		0x00, 0x00, // seq ctl
	})

	// 1. Fixed parameters:
	// Timestamp (8 bytes)
	if err := binary.Write(&buf, binary.LittleEndian, uint64(0)); err != nil {
		return nil, err
	}
	//  Beacon Interval (2 bytes)
	if err := binary.Write(&buf, binary.LittleEndian, r.BeaconInterval); err != nil {
		return nil, err
	}
	//  Capabilities (2 bytes)
	// Capabilities: ESS bit set
	if err := binary.Write(&buf, binary.LittleEndian, uint16(0x0001)); err != nil {
		return nil, err
	}

	// 2. Information Elements (IEs)
	// IE 0: SSID
	buf.WriteByte(IE_SSID)
	buf.WriteByte(byte(len(r.SSID)))
	buf.WriteString(r.SSID)

	// IE 1: Supported Rates
	buf.WriteByte(IE_SUPPORTED_RATES)
	buf.WriteByte(0x08)
	buf.Write([]byte{0x82, 0x84, 0x8B, 0x96, 0x0C, 0x12, 0x18, 0x24}) // 1, 2, 5.5, 11, 6, 9, 12, 18 Mbps

	// IE 3: DS Parameter Set
	buf.WriteByte(IE_DS_PARAM_SET)
	buf.WriteByte(0x01)
	buf.WriteByte(r.Channel)

	return buf.Bytes(), nil
}
