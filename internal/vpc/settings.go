package vpc

import (
	"errors"
	"fmt"
	"net/netip"
)

type Settings struct {
	VPC           netip.Prefix
	PublicSubnet  netip.Prefix
	PrivateSubnet netip.Prefix
	Internet      netip.Prefix
}

func (n Settings) Validate() error {
	for _, field := range []struct {
		name   string
		prefix netip.Prefix
	}{
		{"vpc", n.VPC}, {"publicSubnet", n.PublicSubnet}, {"privateSubnet", n.PrivateSubnet}, {"internet", n.Internet},
	} {
		if !field.prefix.IsValid() || !field.prefix.Addr().Is4() || field.prefix != field.prefix.Masked() {
			return fmt.Errorf("network.%s: expected a canonical IPv4 CIDR", field.name)
		}
	}
	for _, subnet := range []netip.Prefix{n.PublicSubnet, n.PrivateSubnet} {
		if subnet.Bits() <= n.VPC.Bits() || !n.VPC.Contains(subnet.Addr()) {
			return errors.New("network: subnets must be contained within the VPC")
		}
	}
	if n.PublicSubnet.Overlaps(n.PrivateSubnet) {
		return errors.New("network: public and private subnets must not overlap")
	}
	return nil
}
