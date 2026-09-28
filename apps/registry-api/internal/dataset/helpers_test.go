package dataset_test

import "net/netip"

func mustPrefix(s string) netip.Prefix { return netip.MustParsePrefix(s) }
