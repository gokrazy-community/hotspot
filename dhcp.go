package hotspot

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
)

type (
	DHCPHandler interface {
		Discover(DHCPRequest) DHCPReply
		Request(DHCPRequest) DHCPReply
		Err(error)
	}
	DHCPRequest struct {
		HardwareAddr net.HardwareAddr
		Peer         net.Addr // might be 0.0.0.0 on initial broadcat
		WishIP       net.IP   // might be nil
		Hostname     string
	}
	DHCPReply struct {
		IP      net.IP
		Lease   time.Duration
		Subnet  net.IPMask
		Routers []net.IP
		DNS     []net.IP
	}
	DHCPServer struct {
		close func() error
		serve func() error
	}
)

func (d *DHCPServer) Serve() error {
	return d.serve()
}

func (d *DHCPServer) Close() error {
	return d.close()
}

func NewDHCP4(ifname string, handler DHCPHandler) (*DHCPServer, error) {
	server, err := server4.NewServer(ifname, &net.UDPAddr{
		IP:   net.IPv4zero, // all IPv4 addresses
		Port: 67,
	}, func(conn net.PacketConn, peer net.Addr, req *dhcpv4.DHCPv4) {
		log.Println("dhcp4", req)
		if req.OpCode != dhcpv4.OpcodeBootRequest {
			handler.Err(fmt.Errorf("unsupported opcode %d. Only BootRequest (%d) is supported", req.OpCode, dhcpv4.OpcodeBootRequest))
			return
		}
		dreq := DHCPRequest{
			HardwareAddr: req.ClientHWAddr,
			Peer:         peer,
			WishIP:       req.RequestedIPAddress(),
			Hostname:     req.HostName(),
		}
		var modifier dhcpv4.Modifier
		switch mt := req.MessageType(); mt {
		case dhcpv4.MessageTypeDiscover:
			r := handler.Discover(dreq)
			if r.IP.IsUnspecified() {
				handler.Err(fmt.Errorf("Discover: no IP returned"))
				return
			}
			modifier = func(d *dhcpv4.DHCPv4) {
				d.YourIPAddr = r.IP
				d.UpdateOption(dhcpv4.OptIPAddressLeaseTime(r.Lease))
				d.UpdateOption(dhcpv4.OptRouter(r.Routers...))
				d.UpdateOption(dhcpv4.OptSubnetMask(r.Subnet))
				d.UpdateOption(dhcpv4.OptDNS(r.DNS...))
				d.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeOffer))
			}
		case dhcpv4.MessageTypeRequest:
			if dreq.WishIP == nil {
				dreq.WishIP = req.ClientIPAddr
			}
			r := handler.Request(dreq)
			if r.IP.IsUnspecified() {
				handler.Err(fmt.Errorf("Request: no IP returned"))
				return
			}
			modifier = func(d *dhcpv4.DHCPv4) {
				d.YourIPAddr = r.IP
				d.UpdateOption(dhcpv4.OptIPAddressLeaseTime(r.Lease))
				d.UpdateOption(dhcpv4.OptRouter(r.Routers...))
				d.UpdateOption(dhcpv4.OptSubnetMask(r.Subnet))
				d.UpdateOption(dhcpv4.OptDNS(r.DNS...))
				d.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeAck))
			}
		default:
			handler.Err(fmt.Errorf("unsupported message type: %v", mt))
			return
		}
		resp, err := dhcpv4.NewReplyFromRequest(req, modifier)
		if err != nil {
			handler.Err(fmt.Errorf("could not make reply: %w", err))
			return
		}
		if upeer, ok := peer.(*net.UDPAddr); ok && upeer.IP.Equal(net.IPv4zero) {
			peer = &net.UDPAddr{
				IP:   net.IPv4bcast,
				Port: upeer.Port,
				Zone: upeer.Zone,
			}
		}
		_, err = conn.WriteTo(resp.ToBytes(), peer)
		if err != nil {
			handler.Err(fmt.Errorf("could not write reply: %w", err))
			return
		}
		log.Printf("written %v: %v", peer, resp)
	})
	if err != nil {
		return nil, err
	}
	return &DHCPServer{
		close: server.Close,
		serve: server.Serve,
	}, nil
}
