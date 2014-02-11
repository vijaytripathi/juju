// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package upgrades

import "launchpad.net/juju-core/version"

// upgradeOperations returns an ordered slice of sets of operations needed
// to upgrade Juju to particular version. The slice is ordered by target
// version, so that the sets of operations are executed in order from oldest
// version to most recent.
var upgradeOperations = func(context Context) []UpgradeOperation {
	steps := []UpgradeOperation{
		upgradeToVersion{
			version.MustParse("1.18.0"),
			stepsFor118(context),
		},
	}
	return steps
}