package orgmigrate

import "database/sql"

// SnapshotOld exposes the pre-apply snapshot to the external tests.
func SnapshotOld(tx *sql.Tx, rep Report) (matrix, error) { return snapshotOld(tx, rep) }
