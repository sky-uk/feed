package merlin

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

type nl interface {
	addVIP(vipInterface, vip string) error
	removeVIP(vipInterface, vip string) error
}

type nlImpl struct{}

func (i *nlImpl) handleVIP(vipInterface, vip string, fn func(netlink.Link, *netlink.Addr) error) error {
	lnk, err := netlink.LinkByName(vipInterface)
	if err != nil {
		return fmt.Errorf("unable to add/remove VIP on %s: %v", vipInterface, err)
	}
	ipNet, err := netlink.ParseIPNet(vip)
	if err != nil {
		return fmt.Errorf("unable to parse VIP %s: %v", vip, err)
	}
	if err := fn(lnk, &netlink.Addr{IPNet: ipNet, Label: "feed-vip"}); err != nil {
		return fmt.Errorf("unable to add/remove VIP %s to %s: %v", vip, vipInterface, err)
	}
	return nil
}

func (i *nlImpl) addVIP(vipInterface, vip string) error {
	return i.handleVIP(vipInterface, vip, netlink.AddrAdd)
}

func (i *nlImpl) removeVIP(vipInterface, vip string) error {
	return i.handleVIP(vipInterface, vip, netlink.AddrDel)
}
