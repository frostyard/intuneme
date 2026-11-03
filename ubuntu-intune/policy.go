// Package containerimage exposes static image configuration for upgrade reconciliation.
package containerimage

import _ "embed"

// BrokerRestartPolicy authorizes scoped broker restart operations.
//
//go:embed system_files/etc/polkit-1/rules.d/49-intuneme-broker.rules
var BrokerRestartPolicy []byte

// EdgeCommandLineFlagPolicy suppresses Edge command-line security warning banners.
//
//go:embed system_files/etc/opt/edge/policies/managed/intuneme-command-line-flags.json
var EdgeCommandLineFlagPolicy []byte
