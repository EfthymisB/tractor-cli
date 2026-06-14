package models

// BladeCommand is an active command/invocation running on a blade, assembled
// from tractorselect over the invocation, job and command tables.
type BladeCommand struct {
	JID      int
	TID      int
	CID      int
	Owner    string
	Command  string // argv, space-joined
	NumSlots int
}
