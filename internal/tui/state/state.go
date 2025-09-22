package state

type State int

const (
	ActiveProfileList State = iota
	ActiveBucketList
	ActiveObjectList
	Unknow
)
