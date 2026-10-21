// Package containerimage exposes static image configuration for upgrade reconciliation.
package containerimage

import _ "embed"

//go:embed system_files/etc/polkit-1/rules.d/49-intuneme-broker.rules
var BrokerRestartPolicy []byte
