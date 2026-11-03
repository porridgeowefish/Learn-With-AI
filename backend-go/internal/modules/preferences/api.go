// Package preferences is the public facade for learner-owned global preferences.
package preferences

import impl "github.com/xmz14/lll/backend-go/internal/modules/preferences/internal/store"

const Filename = impl.Filename
const MaxBytes = impl.MaxBytes

type Snapshot = impl.Snapshot

func Path() string                           { return impl.Path() }
func Read() (Snapshot, error)                { return impl.Read() }
func Ensure() (Snapshot, error)              { return impl.Ensure() }
func MigrateBackup() error                   { return impl.MigrateBackup() }
func BackupExisting() error                  { return impl.BackupExisting() }
func Write(content string) (Snapshot, error) { return impl.Write(content) }
