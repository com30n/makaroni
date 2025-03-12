package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kaero/makaroni"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func main() {
	InitLogger()
	log.Debug("Application starting (debug level)")

	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	makaroni.LogConfig()

	server, err := SetupServer(config)
	if err != nil {
		log.Fatalf("Error setting up server: %v", err)
	}

	// Start server in a goroutine
	go func() {
		log.Infof("Server started on address %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Setup graceful shutdown
	WaitForShutdown(server)
}

// InitLogger initializes the logger.
func InitLogger() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	logLevel := os.Getenv("LOG_LEVEL")
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		level = log.InfoLevel
		log.Warn("Invalid LOG_LEVEL, defaulting to Info level, supported levels: ", log.AllLevels)
	}
	log.SetLevel(level)
}

// LoadConfig loads the configuration from flags and environment variables.
func LoadConfig() (*makaroni.Config, error) {
	var config makaroni.Config

	rootCmd := &cobra.Command{
		Use:   "makaroni",
		Short: "Makaroni is a paste service",
		Long:  "A web service for sharing code snippets with syntax highlighting",
		Run: func(cmd *cobra.Command, args []string) {
			if err := viper.Unmarshal(&config); err != nil {
				log.Fatalf("Error parsing configuration: %v", err)
			}
		},
	}

	// Define flags
	flags := rootCmd.Flags()
	flags.String("address", "", "Address to serve")
	flags.Int64("multipart-max-memory", 0, "Maximum memory for multipart forms")
	flags.String("index-url", "", "URL to the index page")
	flags.String("result-url-prefix", "", "Upload result URL prefix")
	flags.String("logo-url", "", "Logo URL for the form page")
	flags.String("favicon-url", "", "Favicon URL")
	flags.String("style", "", "Formatting style")
	flags.String("s3-endpoint", "", "S3 endpoint")
	flags.String("s3-region", "", "S3 region")
	flags.String("s3-bucket", "", "S3 bucket")
	flags.String("s3-key-id", "", "S3 key ID")
	flags.String("s3-secret-key", "", "S3 secret key")
	flags.Bool("s3-path-style", false, "S3 use path style addressing")
	flags.Bool("s3-disable-ssl", false, "S3 disable SSL")

	// Bind flags with viper
	if err := viper.BindPFlags(flags); err != nil {
		return nil, fmt.Errorf("error binding flags: %w", err)
	}

	// Setup Viper for environment variables
	viper.SetEnvPrefix("MKRN")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// Bind environment variables automatically
	makaroni.BindEnvVars(config)

	if err := rootCmd.Execute(); err != nil {
		log.Error(err)
		os.Exit(1)
	}

	return &config, nil
}

// SetupServer creates and configures the HTTP server.
func SetupServer(config *makaroni.Config) (*http.Server, error) {
	indexHTML, err := makaroni.RenderIndexPage(config.LogoURL, config.IndexURL, config.FaviconURL)
	if err != nil {
		return nil, fmt.Errorf("failed to render index page: %w", err)
	}

	uploader, err := NewS3Uploader(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 uploader: %w", err)
	}

	mux := SetupRoutes(indexHTML, uploader, config)

	return &http.Server{
		Addr:    config.Address,
		Handler: mux,
	}, nil
}

// NewS3Uploader creates a new S3 uploader.
func NewS3Uploader(config *makaroni.Config) (*makaroni.Uploader, error) {
	log.Info("Initializing uploader")

	uploaderConfig := makaroni.UploaderConfig{
		Endpoint:            config.S3Endpoint,
		DisableSSL:          config.S3DisableSSL,
		PathStyleAddressing: config.S3PathStyle,
		Region:              config.S3Region,
		Bucket:              config.S3Bucket,
		KeyID:               config.S3KeyID,
		Secret:              config.S3SecretKey,
		// TODO: move to config
		// Additional settings
		Timeout:     30 * time.Second,
		PartSize:    5 * 1024 * 1024, // 5MB parts for multipart uploads
		Concurrency: 5,               // 5 concurrent uploads
	}

	uploader, err := makaroni.NewUploader(uploaderConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create uploader: %w", err)
	}

	return uploader, nil
}

// SetupRoutes sets up the HTTP routes.
func SetupRoutes(indexHTML []byte, uploader *makaroni.Uploader, config *makaroni.Config) *http.ServeMux {
	fileServer := http.FileServer(http.Dir("./resources/static"))
	mux := http.NewServeMux()

	// Handle static files
	mux.Handle("/static/", LogStaticFileRequest(http.StripPrefix("/static/", fileServer)))

	// Main handler
	mux.Handle("/", &makaroni.PasteHandler{
		IndexHTML:          indexHTML,
		Uploader:           uploader,
		ResultURLPrefix:    config.ResultURLPrefix,
		Style:              config.Style,
		MultipartMaxMemory: config.MultipartMaxMemory,
		Config:             config,
	})

	// TODO: add an error page

	return mux
}

// LogStaticFileRequest middleware for logging requests to static files
func LogStaticFileRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debug("Received request: ", r.Method, " ", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// WaitForShutdown waits for a shutdown signal and gracefully shuts down the server.
func WaitForShutdown(server *http.Server) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Info("Shutdown signal received, stopping server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Errorf("Server shutdown error: %v", err)
	}

	log.Info("Server stopped successfully")
}
