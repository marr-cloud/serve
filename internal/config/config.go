package config

// Version of the serve binary. Default "dev" for `go install` users;
// release builds override via
// -ldflags "-X github.com/marr-cloud/serve/internal/config.Version=...".
var Version = "dev"

// Config holds all CLI options. Fields map 1-1 to the npm `serve` flags.
type Config struct {
	Directory        string
	Port             int
	Listen           []string
	Single           bool
	Debug            bool
	ConfigFile       string
	NoRequestLogging bool
	CORS             bool
	NoClipboard      bool
	NoCompression    bool
	NoETag           bool
	Symlinks         bool
	NoPortSwitching  bool
	Help             bool
	Version          bool
	SSLCert          string
	SSLKey           string
	SSLPass          string
}
