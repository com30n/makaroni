package main

import (
	"flag"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/kaero/makaroni"
	log "github.com/sirupsen/logrus"
)

func maskSecret(secret string) string {
	if len(secret) <= 6 {
		if len(secret) <= 2 {
			return secret
		}
		return secret[:1] + strings.Repeat("*", len(secret)-2) + secret[len(secret)-1:]
	}
	return secret[:3] + strings.Repeat("*", len(secret)-6) + secret[len(secret)-3:]
}

func initLogger() {
	// Set log format and level
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	// Set logging level (adjust as needed, or load from env/config)
	logLevel := os.Getenv("LOG_LEVEL")
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		level = log.InfoLevel
		log.Warn("Invalid LOG_LEVEL, defaulting to Info level, supported levels: ", log.AllLevels)
	}
	log.SetLevel(level)
}

func main() {
	initLogger()
	log.Debug("Application starting (debug level)")

	// Debug: output environment variable values, masking secrets
	log.Debugf("MKRN_ADDRESS: %s", os.Getenv("MKRN_ADDRESS"))
	log.Debugf("MKRN_MULTIPART_MAX_MEMORY: %s", os.Getenv("MKRN_MULTIPART_MAX_MEMORY"))
	log.Debugf("MKRN_INDEX_URL: %s", os.Getenv("MKRN_INDEX_URL"))
	log.Debugf("MKRN_RESULT_URL_PREFIX: %s", os.Getenv("MKRN_RESULT_URL_PREFIX"))
	log.Debugf("MKRN_LOGO_URL: %s", os.Getenv("MKRN_LOGO_URL"))
	log.Debugf("MKRN_FAVICON_URL: %s", os.Getenv("MKRN_FAVICON_URL"))
	log.Debugf("MKRN_STYLE: %s", os.Getenv("MKRN_STYLE"))
	log.Debugf("MKRN_S3_ENDPOINT: %s", os.Getenv("MKRN_S3_ENDPOINT"))
	log.Debugf("MKRN_S3_REGION: %s", os.Getenv("MKRN_S3_REGION"))
	log.Debugf("MKRN_S3_BUCKET: %s", os.Getenv("MKRN_S3_BUCKET"))
	log.Debugf("MKRN_S3_KEY_ID: %s", os.Getenv("MKRN_S3_KEY_ID"))
	log.Debugf("MKRN_S3_SECRET_KEY: %s", maskSecret(os.Getenv("MKRN_S3_SECRET_KEY")))
	log.Debugf("MKRN_S3_PATH_STYLE: %t", os.Getenv("MKRN_S3_PATH_STYLE"))
	log.Debugf("MKRN_S3_DISABLE_SSL: %t", os.Getenv("MKRN_S3_DISABLE_SSL"))

	address := flag.String("address", os.Getenv("MKRN_ADDRESS"), "Address to serve")
	multipartMaxMemoryEnv, err := strconv.ParseInt(os.Getenv("MKRN_MULTIPART_MAX_MEMORY"), 0, 64)
	if err != nil {
		log.Error("Error parsing MKRN_MULTIPART_MAX_MEMORY: ", err)
		log.Fatal(err)
	}
	s3PathStyleAddressing, err := strconv.ParseBool(os.Getenv("MKRN_S3_PATH_STYLE"))
	if err != nil {
		log.Error("Error parsing MKRN_S3_PATH_STYLE: ", err)
		log.Fatal(err)
	}
	s3DisableSSL, err := strconv.ParseBool(os.Getenv("MKRN_S3_DISABLE_SSL"))
	if err != nil {
		log.Error("Error parsing MKRN_S3_DISABLE_SSL: ", err)
		log.Fatal(err)
	}
	log.Debugf("Parsed multipartMaxMemory: %d", multipartMaxMemoryEnv)
	multipartMaxMemory := flag.Int64("multipart-max-memory", multipartMaxMemoryEnv, "Maximum memory for multipart form parser")
	indexURL := flag.String("index-url", os.Getenv("MKRN_INDEX_URL"), "URL to the index page")
	resultURLPrefix := flag.String("result-url-prefix", os.Getenv("MKRN_RESULT_URL_PREFIX"), "Upload result URL prefix.")
	logoURL := flag.String("logo-url", os.Getenv("MKRN_LOGO_URL"), "Logo URL for the form page")
	faviconURL := flag.String("favicon-url", os.Getenv("MKRN_FAVICON_URL"), "Favicon URL")
	style := flag.String("style", os.Getenv("MKRN_STYLE"), "Formatting style")
	s3Endpoint := flag.String("s3-endpoint", os.Getenv("MKRN_S3_ENDPOINT"), "S3 endpoint")
	s3Region := flag.String("s3-region", os.Getenv("MKRN_S3_REGION"), "S3 region")
	s3Bucket := flag.String("s3-bucket", os.Getenv("MKRN_S3_BUCKET"), "S3 bucket")
	s3KeyID := flag.String("s3-key-id", os.Getenv("MKRN_S3_KEY_ID"), "S3 key ID")
	s3SecretKey := flag.String("s3-secret-key", os.Getenv("MKRN_S3_SECRET_KEY"), "S3 secret key")
	help := flag.Bool("help", false, "Print usage")
	flag.Parse()
	log.Debug("Flags parsed successfully.")

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	log.Info("Rendering index page")
	indexHTML, err := makaroni.RenderIndexPage(*logoURL, *indexURL, *faviconURL)
	if err != nil {
		log.Error("Error rendering index page: ", err)
		log.Fatal(err)
	}
	log.Debug("Index page rendered successfully.")

	log.Info("Rendering output pre HTML")
	outputPreHTML, err := makaroni.RenderOutputPre(*logoURL, *indexURL, *faviconURL)
	if err != nil {
		log.Error("Error rendering output pre HTML: ", err)
		log.Fatal(err)
	}
	log.Debug("Output pre HTML rendered successfully.")

	log.Info("Initializing uploader")
	uploadFunc, err := makaroni.NewUploader(*s3Endpoint, s3DisableSSL, s3PathStyleAddressing, *s3Region, *s3Bucket, *s3KeyID, *s3SecretKey)
	if err != nil {
		log.Error("Error initializing uploader: ", err)
		log.Fatal(err)
	}
	log.Debug("Uploader initialized successfully.")

	mux := http.NewServeMux()
	mux.Handle("/", &makaroni.PasteHandler{
		IndexHTML:          indexHTML,
		OutputHTMLPre:      outputPreHTML,
		Upload:             uploadFunc,
		ResultURLPrefix:    *resultURLPrefix,
		Style:              *style,
		MultipartMaxMemory: *multipartMaxMemory,
	})
	log.Debug("HTTP multiplexer configured.")

	server := http.Server{
		Addr:    *address,
		Handler: mux,
	}
	log.Info("Server starting on address ", *address)
	if err := server.ListenAndServe(); err != nil {
		log.Error("Server stopped with error: ", err)
		log.Fatal(err)
	}
}
