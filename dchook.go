package dchook

var (
	version = "dev"
	commit  = "none"
)

// Version returns the version and commit information.
func Version() string {
	return version + " (" + commit + ")"
}
