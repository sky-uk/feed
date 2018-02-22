package merlin

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

// netlinkWrapper is an interface to mock out netlink interactions for tests.
type netlinkWrapper interface {
	addVIP(vipInterface, vip string) error
	removeVIP(vipInterface, vip string) error
}

type netlinkWrapperImpl struct{}

func (i *netlinkWrapperImpl) handleVIP(vipInterface, vip string, fn func(netlink.Link, *netlink.Addr) error) error {
	ipNet, err := netlink.ParseIPNet(vip)
	if err != nil {
		return fmt.Errorf("unable to parse VIP %s: %v", vip, err)
	}
	lnk, err := netlink.LinkByName(vipInterface)
	if err != nil {
		return fmt.Errorf("unable to add/remove VIP on %s: %v", vipInterface, err)
	}
	if err := fn(lnk, &netlink.Addr{IPNet: ipNet, Label: "feed-vip"}); err != nil {
		return fmt.Errorf("unable to add/remove VIP %s to %s: %v", vip, vipInterface, err)
	}
	return nil
}

func (i *netlinkWrapperImpl) addVIP(vipInterface, vip string) error {
	return i.handleVIP(vipInterface, vip, netlink.AddrAdd)
}

func (i *netlinkWrapperImpl) removeVIP(vipInterface, vip string) error {
	return i.handleVIP(vipInterface, vip, netlink.AddrDel)
}
