package models

// Blade represents a render farm worker node, as returned by q=blades.
type Blade struct {
	Name       string   // hostname / blade name
	Addr       string   // ip address
	Port       int      // listening port
	Profile    string   // tractor profile
	Version    string   // blade version
	OSName     string   // operating system
	OSVersion  string   // os version
	GPULabel   string   // gpu info
	Nimby      string   // nimby state (empty = not nimbied)
	Note       string   // status note
	Heartbeat  float64  // last heartbeat, epoch seconds
	StartTime  float64  // blade process start, epoch seconds
	NumCPU     int      // cpu cores
	NumSlots   int      // total slots
	SlotsInUse int      // slots in use
	AvailSlots int      // free slots
	TaskCount  int      // active task count
	LoadAvg    float64  // cpu load average
	MemGB      float64  // available memory (GB)
	DiskGB     float64  // available disk (GB)
	UDI        float64  // universal desirability index
	Owners     []string // owners of active tasks
	BladeID    string   // blade uuid
}
