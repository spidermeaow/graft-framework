package cli

import (
	"context"
	"fmt"
	"github.com/spidermeaow/graft-framework/migration"
	mysqlstore "github.com/spidermeaow/graft-framework/migration/mysql"
	"io"
)

func mysqlDoctor(ctx context.Context, store *mysqlstore.Store, migrations []migration.Migration, out io.Writer) error {
	dirty, err := store.Inspect(ctx)
	if err != nil {
		return err
	}
	if len(dirty) == 0 {
		fmt.Fprintln(out, "No dirty MySQL migrations. Run graft migrate:status for full history.")
		return nil
	}
	local := make(map[int64]migration.Migration, len(migrations))
	for _, m := range migrations {
		local[m.Version] = m
	}
	for _, d := range dirty {
		fmt.Fprintf(out, "DIRTY %d_%s (%s), last event: %s\n", d.Version, d.Name, d.Direction, d.LastEvent)
		if m, ok := local[d.Version]; !ok || m.Name != d.Name || m.Checksum != d.Checksum {
			fmt.Fprintln(out, "  Local SQL is missing or differs from recorded name/checksum; restore the exact file before repair.")
		}
		fmt.Fprintln(out, "  Back up and inspect actual schema/data. Restore fully to pending or applied state before changing history.")
		fmt.Fprintf(out, "  Preview: graft migrate:repair --mark-pending %d --plan (or --mark-applied %d --plan)\n", d.Version, d.Version)
	}
	return &mysqlstore.DirtyError{Version: dirty[0].Version, Direction: dirty[0].Direction}
}
