package models

// Task represents a single task within a Tractor job, as returned by the
// engine's q=jtree query (the task tree of a job).
type Task struct {
	TID        int     // task id
	JID        int     // owning job id
	Title      string  // task title
	State      string  // decoded: blocked/active/error/done
	Service    string  // service key expression
	Progress   float64 // 0–100, decoded from the state field
	HasLog     bool    // whether the task has a log
	Blade      string  // blade that ran the current invocation, if any
	StateTime  float64 // epoch seconds of last state change
	ActiveTime float64 // epoch seconds the task went active (0 if never)
}

// TaskDetail holds the per-task command/invocation details shown in the Tasks
// info panel, taken from the task's primary command (q=taskdetails).
type TaskDetail struct {
	Command    string   // argv of the command, space-joined
	Blades     []string // blades the command ran on
	Service    string   // service key expression
	EnvKey     string   // env key(s), comma-joined
	RSSMB      float64   // resident set size, MB
	VSZMB      float64   // virtual size, MB
	CPU        float64   // cpu usage, percent
	Slots      int       // slots in use (numslots of current invocation)
	MinSlots   int       // command min slots
	MaxSlots   int       // command max slots
	MinRunSecs int       // command min run seconds
	MaxRunSecs int       // command max run seconds
}
