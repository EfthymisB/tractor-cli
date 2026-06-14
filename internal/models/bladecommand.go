package models

// BladeCommand is an active command/invocation running on a blade, as returned
// in the "cmds" array of q=bladedetails.
type BladeCommand struct {
	JID       int
	TID       int
	CID       int
	Owner     string
	Command   string  // argv, space-joined
	StartTime float64 // epoch seconds
	NumSlots  int
}
