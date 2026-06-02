package config

// Version of the serve binary. Updated per release.
const Version = "0.1.0"

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
