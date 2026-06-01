package main

import (
	"compress/gzip"
	"context"
	"crypto/md5"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tiagomelo/go-clipboard/clipboard"
)

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
}

const VERSION = "1.0.0"

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func main() {
	config := parseFlags()

	if config.Help {
		printHelp()
		return
	}

	if config.Version {
		fmt.Printf("serve version %s\n", VERSION)
		return
	}

	// Set default directory to current working directory
	if config.Directory == "" {
		var err error
		config.Directory, err = os.Getwd()
		if err != nil {
			log.Fatal("Failed to get current directory:", err)
		}
	}

	// Validate directory exists
	if _, err := os.Stat(config.Directory); os.IsNotExist(err) {
		log.Fatalf("Directory '%s' does not exist", config.Directory)
	}

	// Set default listen addresses if none specified
	if len(config.Listen) == 0 {
		if config.Port != 0 {
			config.Listen = []string{fmt.Sprintf("0.0.0.0:%d", config.Port)}
		} else {
			config.Listen = []string{"0.0.0.0:3000"}
		}
	}

	server := &Server{config: config}
	server.Start()
}

func parseFlags() Config {
	var config Config
	var listenFlags arrayFlags

	flag.Var(&listenFlags, "l", "Specify a URI endpoint on which to listen")
	flag.Var(&listenFlags, "listen", "Specify a URI endpoint on which to listen")
	flag.IntVar(&config.Port, "p", 0, "Specify custom port")
	flag.BoolVar(&config.Single, "s", false, "Rewrite all not-found requests to `index.html`")
	flag.BoolVar(&config.Single, "single", false, "Rewrite all not-found requests to `index.html`")
	flag.BoolVar(&config.Debug, "d", false, "Show debugging information")
	flag.BoolVar(&config.Debug, "debug", false, "Show debugging information")
	flag.StringVar(&config.ConfigFile, "c", "", "Specify custom path to `serve.json`")
	flag.StringVar(&config.ConfigFile, "config", "", "Specify custom path to `serve.json`")
	flag.BoolVar(&config.NoRequestLogging, "L", false, "Do not log any request information to the console")
	flag.BoolVar(&config.NoRequestLogging, "no-request-logging", false, "Do not log any request information to the console")
	flag.BoolVar(&config.CORS, "C", false, "Enable CORS, sets `Access-Control-Allow-Origin` to `*`")
	flag.BoolVar(&config.CORS, "cors", false, "Enable CORS, sets `Access-Control-Allow-Origin` to `*`")
	flag.BoolVar(&config.NoClipboard, "n", false, "Do not copy the local address to the clipboard")
	flag.BoolVar(&config.NoClipboard, "no-clipboard", false, "Do not copy the local address to the clipboard")
	flag.BoolVar(&config.NoCompression, "u", false, "Do not compress files")
	flag.BoolVar(&config.NoCompression, "no-compression", false, "Do not compress files")
	flag.BoolVar(&config.NoETag, "no-etag", false, "Send `Last-Modified` header instead of `ETag`")
	flag.BoolVar(&config.Symlinks, "S", false, "Resolve symlinks instead of showing 404 errors")
	flag.BoolVar(&config.Symlinks, "symlinks", false, "Resolve symlinks instead of showing 404 errors")
	flag.BoolVar(&config.NoPortSwitching, "no-port-switching", false, "Do not open a port other than the one specified when it's taken")
	flag.BoolVar(&config.Help, "help", false, "Shows this help message")
	flag.BoolVar(&config.Version, "v", false, "Displays the current version of serve")
	flag.BoolVar(&config.Version, "version", false, "Displays the current version of serve")

	flag.Parse()

	config.Listen = []string(listenFlags)

	// Get directory from remaining arguments
	args := flag.Args()
	if len(args) > 0 {
		config.Directory = args[0]
	}

	return config
}

type arrayFlags []string

func (i *arrayFlags) String() string {
	return fmt.Sprintf("%v", *i)
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

type Server struct {
	config Config
}

func (s *Server) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)

	var servers []*http.Server
	var localAddr string

	for _, addr := range s.config.Listen {
		// Parse address
		if !strings.Contains(addr, ":") {
			addr = "0.0.0.0:" + addr
		}

		server := &http.Server{
			Addr:    addr,
			Handler: mux,
		}

		servers = append(servers, server)

		go func(srv *http.Server, address string) {
			listener, err := net.Listen("tcp", address)
			if err != nil {
				if !s.config.NoPortSwitching && strings.Contains(err.Error(), "address already in use") {
					// Try to find an available port
					if port := s.findAvailablePort(address); port != "" {
						listener, err = net.Listen("tcp", port)
						srv.Addr = port
					}
				}
				if err != nil {
					log.Printf("Failed to listen on %s: %v", address, err)
					return
				}
			}

			if strings.HasPrefix(srv.Addr, "0.0.0.0:") || strings.HasPrefix(srv.Addr, ":") {
				port := strings.Split(srv.Addr, ":")[1]
				localAddr = "http://localhost:" + port
			}

			if s.config.Debug {
				log.Printf("Starting server on %s", srv.Addr)
			}

			if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
				log.Printf("Server error on %s: %v", srv.Addr, err)
			}
		}(server, addr)
	}

	// Print startup message
	s.printStartupMessage(localAddr)

	// Copy to clipboard
	if !s.config.NoClipboard && localAddr != "" {
		c := clipboard.New()
		if err := c.CopyText(localAddr); err == nil {
			// Clipboard copy successful (already shown in startup message)
		}
	}

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("\n INFO  Gracefully shutting down. Please wait...")

	// Shutdown servers
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, server := range servers {
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}
}

func (s *Server) findAvailablePort(addr string) string {
	host := strings.Split(addr, ":")[0]
	basePort := 3000
	if strings.Contains(addr, ":") {
		if p, err := strconv.Atoi(strings.Split(addr, ":")[1]); err == nil {
			basePort = p + 1
		}
	}

	for port := basePort; port < basePort+100; port++ {
		testAddr := fmt.Sprintf("%s:%d", host, port)
		if listener, err := net.Listen("tcp", testAddr); err == nil {
			listener.Close()
			return testAddr
		}
	}
	return ""
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	if !s.config.NoRequestLogging {
		log.Printf("%s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
	}

	if s.config.CORS {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
	}

	if r.Method == "OPTIONS" {
		return
	}

	// Clean the path
	urlPath := filepath.Clean(r.URL.Path)
	if urlPath == "." {
		urlPath = "/"
	}

	// Convert URL path to file system path
	filePath := filepath.Join(s.config.Directory, strings.TrimPrefix(urlPath, "/"))

	// Check if path exists (handle symlinks based on config)
	var info os.FileInfo
	var err error
	if s.config.Symlinks {
		info, err = os.Stat(filePath) // Follow symlinks
	} else {
		info, err = os.Lstat(filePath) // Don't follow symlinks
	}
	
	if err != nil {
		if os.IsPermission(err) {
			http.Error(w, "Permission denied", http.StatusForbidden)
			return
		}
		if s.config.Single {
			// Serve index.html for SPA
			indexPath := filepath.Join(s.config.Directory, "index.html")
			s.serveFile(w, r, indexPath)
			return
		}
		http.NotFound(w, r)
		return
	}

	// If symlinks are disabled and this is a symlink, return 404
	if !s.config.Symlinks && info.Mode()&os.ModeSymlink != 0 {
		http.NotFound(w, r)
		return
	}

	// If it's a directory, try to serve index.html
	if info.IsDir() {
		indexPath := filepath.Join(filePath, "index.html")
		if indexInfo, err := os.Stat(indexPath); err == nil && !indexInfo.IsDir() {
			// Check if we can read the index.html file
			if file, err := os.Open(indexPath); err == nil {
				file.Close()
				s.serveFile(w, r, indexPath)
				return
			} else if os.IsPermission(err) {
				http.Error(w, "Permission denied", http.StatusForbidden)
				return
			}
		}
		// Serve directory listing
		s.serveDirectory(w, r, filePath, urlPath)
		return
	}

	// Serve the file
	s.serveFile(w, r, filePath)
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, filePath string) {
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsPermission(err) {
			http.Error(w, "Permission denied", http.StatusForbidden)
			return
		}
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Set content type
	contentType := getContentType(filePath)
	w.Header().Set("Content-Type", contentType)

	// Set ETag or Last-Modified
	if s.config.NoETag {
		w.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
	} else {
		etag := s.generateETag(info, filePath)
		w.Header().Set("ETag", etag)
		
		// Check If-None-Match
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	// Handle compression
	if !s.config.NoCompression && acceptsGzip(r) && shouldCompress(contentType) {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		io.Copy(gz, file)
		return
	}

	// Serve file normally
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *Server) serveDirectory(w http.ResponseWriter, r *http.Request, dirPath, urlPath string) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		http.Error(w, "Unable to read directory", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>Index of %s</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        h1 { border-bottom: 1px solid #ccc; }
        a { text-decoration: none; color: #0066cc; }
        a:hover { text-decoration: underline; }
        .file { margin: 5px 0; }
    </style>
</head>
<body>
    <h1>Index of %s</h1>
`, urlPath, urlPath)

	if urlPath != "/" {
		html += `    <div class="file"><a href="../">../</a></div>` + "\n"
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		// Use path.Join for URLs to ensure forward slashes
		href := path.Join(urlPath, name)
		if entry.IsDir() && !strings.HasSuffix(href, "/") {
			href += "/"
		}
		html += fmt.Sprintf(`    <div class="file"><a href="%s">%s</a></div>`, href, name) + "\n"
	}

	html += `</body>
</html>`

	fmt.Fprint(w, html)
}

func acceptsGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}

func shouldCompress(contentType string) bool {
	compressible := []string{
		"text/",
		"application/javascript",
		"application/json",
		"application/xml",
		"application/xhtml+xml",
	}
	
	for _, prefix := range compressible {
		if strings.HasPrefix(contentType, prefix) {
			return true
		}
	}
	return false
}

func getContentType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	
	// Fast path for common extensions
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".xml":
		return "application/xml; charset=utf-8"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	}
	
	// Fallback to http.DetectContentType for unknown extensions
	if file, err := os.Open(filePath); err == nil {
		defer file.Close()
		
		// Read first 512 bytes for content detection
		buffer := make([]byte, 512)
		if n, err := file.Read(buffer); err == nil {
			contentType := http.DetectContentType(buffer[:n])
			
			// Add charset for text content types
			if strings.HasPrefix(contentType, "text/") && !strings.Contains(contentType, "charset") {
				contentType += "; charset=utf-8"
			}
			
			return contentType
		}
	}
	
	// Ultimate fallback
	return "application/octet-stream"
}

func (s *Server) generateETag(info os.FileInfo, filePath string) string {
	// Basic ETag based on mod time and size (fast)
	basicETag := fmt.Sprintf(`"%x-%x"`, info.ModTime().Unix(), info.Size())
	
	// For small files (< 1MB), optionally include content hash for better uniqueness
	if info.Size() < 1024*1024 && s.config.Debug {
		if file, err := os.Open(filePath); err == nil {
			defer file.Close()
			hash := md5.New()
			if _, err := io.Copy(hash, file); err == nil {
				contentHash := fmt.Sprintf("%x", hash.Sum(nil))
				return fmt.Sprintf(`"%s-%s"`, contentHash[:8], basicETag[1:len(basicETag)-1])
			}
		}
	}
	
	return basicETag
}

func (s *Server) printStartupMessage(localAddr string) {
	fmt.Println("   ┌─────────────────────────────────────────────┐")
	fmt.Println("   │                                             │")
	fmt.Println("   │   Serving!                                  │")
	fmt.Println("   │                                             │")
	
	if localAddr != "" {
		fmt.Printf("   │   - Local:    %-30s│\n", localAddr)
	}
	
	for _, addr := range s.config.Listen {
		if strings.HasPrefix(addr, "0.0.0.0:") {
			// Get network IP
			if ip := getOutboundIP(); ip != "" {
				port := strings.Split(addr, ":")[1]
				networkAddr := fmt.Sprintf("http://%s:%s", ip, port)
				fmt.Printf("   │   - Network:  %-30s│\n", networkAddr)
			}
		}
	}
	
	fmt.Println("   │                                             │")
	
	if !s.config.NoClipboard && localAddr != "" {
		fmt.Println("   │   Copied local address to clipboard!        │")
		fmt.Println("   │                                             │")
	}
	
	fmt.Println("   └─────────────────────────────────────────────┘")
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

func printHelp() {
	fmt.Println(`  serve - Static file serving and directory listing

  USAGE
    $ serve --help
    $ serve --version  
    $ serve folder_name
    $ serve [-l listen_uri [-l ...]] [directory]

    By default, serve will listen on 0.0.0.0:3000 and serve the
    current working directory on that address.

    Specifying a single --listen argument will overwrite the default, not supplement it.

  OPTIONS
    --help                              Shows this help message
    -v, --version                       Displays the current version of serve
    -l, --listen listen_uri             Specify a URI endpoint on which to listen -
                                        more than one may be specified to listen in multiple places
    -p                                  Specify custom port
    -s, --single                        Rewrite all not-found requests to ` + "`index.html`" + `
    -d, --debug                         Show debugging information
    -c, --config                        Specify custom path to ` + "`serve.json`" + `
    -L, --no-request-logging            Do not log any request information to the console
    -C, --cors                          Enable CORS, sets ` + "`Access-Control-Allow-Origin`" + ` to ` + "`*`" + `
    -n, --no-clipboard                  Do not copy the local address to the clipboard
    -u, --no-compression                Do not compress files
    --no-etag                           Send ` + "`Last-Modified`" + ` header instead of ` + "`ETag`" + `
    -S, --symlinks                      Resolve symlinks instead of showing 404 errors
    --no-port-switching                 Do not open a port other than the one specified when it's taken

  ENDPOINTS
    Listen endpoints (specified by the --listen or -l options above) instruct serve
    to listen on one or more interfaces/ports, UNIX domain sockets, or Windows named pipes.

    For TCP ports on hostname "localhost":
      $ serve -l 1234`)
}
