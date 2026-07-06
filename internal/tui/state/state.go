// Package state enumerates the TUI's active-list states (profile/bucket/object).
package state

type State int

const (
	ActiveProfileList State = iota
	ActiveBucketList
	ActiveObjectList
	Unknown
)
