package vpc

import (
	"errors"
	"fmt"
	"net/netip"
)

type Settings struct {
	VPC           netip.Prefix
	PrivateSubnet netip.Prefix
}

func (n Settings) Validate() error {
	for _, field := range []struct {
		name   string
		prefix netip.Prefix
	}{
		{"vpc", n.VPC}, {"privateSubnet", n.PrivateSubnet},
	} {
		if !field.prefix.IsValid() || !field.prefix.Addr().Is4() || field.prefix != field.prefix.Masked() {
			return fmt.Errorf("network.%s: expected a canonical IPv4 CIDR", field.name)
		}
	}
	if n.PrivateSubnet.Bits() <= n.VPC.Bits() || !n.VPC.Contains(n.PrivateSubnet.Addr()) {
		return errors.New("network: private subnet must be contained within the VPC")
	}
	return nil
}
