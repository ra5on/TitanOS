package viewer

import (
	"context"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

// GuardTable is the nft table that keeps viewer networks away from the
// host. It is TitanOS's own and independent of the firewall (on or off).
const GuardTable = "titanos_viewer"

// guardScript replaces the table in one transaction: from the bridges of
// viewer networks (tvw…) the host accepts only replies to connections it
// opened itself (titand dials the agent); everything new is dropped, so a
// browser in a viewer reaches no service of the NAS through the gateway.
func guardScript() string {
	return "table inet " + GuardTable + "\n" +
		"delete table inet " + GuardTable + "\n" +
		"table inet " + GuardTable + " {\n" +
		"\tchain input {\n" +
		"\t\ttype filter hook input priority -10; policy accept;\n" +
		"\t\tiifname \"" + BridgePrefix + "*\" ct state established,related accept\n" +
		"\t\tiifname \"" + BridgePrefix + "*\" drop\n" +
		"\t}\n" +
		"}\n"
}

// HostGuard returns a function that loads the rules with nft through
// runner. The table is replaced each time (cheap, idempotent), so a flushed
// ruleset is repaired with the next view.
func HostGuard(runner run.Runner) func(context.Context) error {
	return func(ctx context.Context) error {
		_, err := runner.Run(ctx, guardScript(), "nft", "-f", "-")
		return err
	}
}
