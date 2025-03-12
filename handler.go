package makaroni

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

const (
	pasteDataCookieName = "paste_data"
	cookieMaxAge        = 86400 * 365 // 365 days
	contentTypeHTML     = "text/html"
	contentTypeText     = "text/plain"
)

var (
	ErrEmptyFormContent = errors.New("empty form content")
)

// PasteHandler structure for handling uploads
type PasteHandler struct {
	IndexHTML          []byte
	Uploader           *Uploader
	Style              string
	ResultURLPrefix    string
	MultipartMaxMemory int64
	Config             *Config
}

// PasteObject represents a single uploaded object data
type PasteObject struct {
	HtmlKey   string `json:"htmlKey"`
	RawKey    string `json:"rawKey"`
	DeleteKey string `json:"deleteKey"`
}

// PasteData represents the data stored in cookies about uploaded pastes
type PasteData struct {
	Objects    []PasteObject `json:"objects"` // Array of uploaded objects
	CreateTime time.Time     `json:"create_time"`
}

// SetCommonHeaders sets common headers for the response.
func SetCommonHeaders(w http.ResponseWriter, contentType string) {
	w.Header().Set("Content-Type", contentType)
}

// setCookies adds a cookie with base64-encoded JSON data
func (p *PasteHandler) setCookies(w http.ResponseWriter, keyRaw, keyHtml, keyDelete string) {
	// Create data structure for the cookie
	pasteData := PasteData{
		Objects: []PasteObject{
			{
				HtmlKey:   keyHtml,
				RawKey:    keyRaw,
				DeleteKey: keyDelete,
			},
		},
		CreateTime: time.Now().UTC(),
	}

	// Serialize to JSON
	jsonData, err := json.Marshal(pasteData)
	if err != nil {
		log.Error("Failed to serialize paste data:", err)
		return
	}

	// Encode JSON to base64
	encodedData := base64.StdEncoding.EncodeToString(jsonData)

	// Set cookie with encoded paste data
	pasteCookie := &http.Cookie{
		Name:     pasteDataCookieName,
		Value:    encodedData,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   cookieMaxAge,
	}

	http.SetCookie(w, pasteCookie)
	log.Debug("Set base64-encoded paste_data cookie")
}

// ServeHTTP handles HTTP requests using different log levels.
func (p *PasteHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	log.Info("Received request: ", req.Method, " ", req.URL.Path)

	switch req.Method {
	case http.MethodGet:
		p.handleGetRequest(w)
	case http.MethodPost:
		p.handlePostRequest(w, req)
	case http.MethodDelete:
		p.handleDeleteRequest(w, req)
	default:
		log.Warn("Unsupported request method: ", req.Method)
		p.RespondWithError(w, http.StatusBadRequest, "Unsupported method", p.Config)
	}
}

// handleGetRequest handles GET requests by sending the index page
func (p *PasteHandler) handleGetRequest(w http.ResponseWriter) {
	log.Info("Sending index page")
	SetCommonHeaders(w, contentTypeHTML)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(p.IndexHTML); err != nil {
		log.Error("Error sending indexHTML: ", err)
	}
}

// handlePostRequest handles POST requests for uploading content
func (p *PasteHandler) handlePostRequest(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseMultipartForm(p.MultipartMaxMemory); err != nil {
		log.Warn("Error parsing form: ", err)
		p.RespondWithError(w, http.StatusBadRequest, "Invalid form", p.Config)
		return
	}

	keyRaw, keyHtml, keyDelete, err := p.generateKeys()
	if err != nil {
		p.RespondWithError(w, http.StatusInternalServerError, "Failed to generate keys", p.Config)
		return
	}

	metadata := map[string]*string{
		"delete": &keyDelete,
	}

	urlHTML := p.ResultURLPrefix + keyHtml
	urlRaw := p.ResultURLPrefix + keyRaw

	content, file, header, err := p.getFormContent(w, req)
	if err != nil {
		if errors.Is(err, ErrEmptyFormContent) {
			return // getFormContent already handles the redirect
		}
		p.RespondWithError(w, http.StatusInternalServerError, "Failed to get form content", p.Config)
		return
	}

	var html string
	if file != nil {
		html, err = p.processFileUpload(req, file, header, keyRaw, metadata)
	} else {
		html, err = p.processTextUpload(req, content, keyRaw, urlRaw, metadata)
	}

	if err != nil {
		p.RespondWithError(w, http.StatusInternalServerError, "Failed to process upload", p.Config)
		return
	}

	if err = p.Uploader.UploadString(req.Context(), keyHtml, html, contentTypeHTML, metadata); err != nil {
		log.Error("Error uploading HTML: ", err)
		p.RespondWithError(w, http.StatusInternalServerError, "Failed to upload HTML content", p.Config)
		return
	}

	log.Info("Uploaded HTML content with key: ", keyHtml)

	// Set cookie with paste data
	p.setCookies(w, keyRaw, keyHtml, keyDelete)

	p.redirectToURL(w, req, urlHTML)
	log.Debug("Redirecting to URL: ", urlHTML)
}

// handleDeleteRequest handles DELETE requests to remove pastes
func (p *PasteHandler) handleDeleteRequest(w http.ResponseWriter, req *http.Request) {
	// Get all keys from query parameters
	rawKey := req.URL.Query().Get("raw")
	htmlKey := req.URL.Query().Get("html")
	deleteKey := req.URL.Query().Get("key")

	if rawKey == "" || deleteKey == "" || htmlKey == "" {
		log.Warn("Missing required parameters for deletion")
		p.RespondWithError(w, http.StatusBadRequest, "Missing required parameters", p.Config)
		return
	}

	log.Info("Deleting paste with rawKey: ", rawKey, ", using deleteKey: ", deleteKey)

	// Prepare list of keys to delete
	keysToDelete := []string{rawKey, htmlKey}
	for _, key := range keysToDelete {
		metadata, err := p.Uploader.GetMetadata(req.Context(), key)
		if err != nil {
			log.Error("Error retrieving metadata: ", err)
			p.RespondWithError(w, http.StatusNotFound, "Metadata not found", p.Config)
			return
		}

		storedDeleteKey, exists := metadata["delete"]
		if !exists || storedDeleteKey == nil || *storedDeleteKey != deleteKey {
			log.Warn("Invalid delete key provided for: ", rawKey)
			p.RespondWithError(w, http.StatusForbidden, "Invalid delete key", p.Config)
			return
		}
	}

	// Delete all objects in a single batch request
	if err := p.Uploader.DeleteObjects(req.Context(), keysToDelete); err != nil {
		log.Error("Error deleting objects: ", err)
		p.RespondWithError(w, http.StatusInternalServerError, "Failed to delete objects", p.Config)
		return
	}

	log.Info("Successfully deleted paste with key: ", rawKey)
	w.WriteHeader(http.StatusOK)
}

// generateKeys generates unique keys for raw and HTML content
func (p *PasteHandler) generateKeys() (string, string, string, error) {
	uuidV4, err := uuid.NewRandom()
	deleteUuid, err := uuid.NewRandom()
	if err != nil {
		log.Error("Error generating UUID: ", err)
		return "", "", "", err
	}
	keyRaw := uuidV4.String()
	keyHtml := keyRaw + ".html"
	keyDelete := deleteUuid.String()
	return keyRaw, keyHtml, keyDelete, nil
}

// processFileUpload handles file upload and returns the rendered HTML
func (p *PasteHandler) processFileUpload(req *http.Request, file multipart.File, header *multipart.FileHeader, keyRaw string, metadata map[string]*string) (string, error) {
	fileExtension := filepath.Ext(header.Filename)
	contentType := header.Header.Get("Content-Type")

	if len(fileExtension) > 0 {
		keyRaw = keyRaw + fileExtension
	}

	if err := p.Uploader.UploadReader(req.Context(), keyRaw, file, contentType, metadata); err != nil {
		log.Error("Error uploading file: ", err)
		return "", err
	}

	log.Info("Uploaded file with key: ", keyRaw)
	log.Debug("File Size: " + fmt.Sprintf("%d", header.Size))
	log.Debug("MIME Header: " + header.Header.Get("Content-Type"))

	data := FileDownloadData{
		LogoURL:     p.Config.LogoURL,
		IndexURL:    p.Config.IndexURL,
		FaviconURL:  p.Config.FaviconURL,
		FileName:    header.Filename,
		DownloadURL: keyRaw,
		CanView:     CanViewInBrowser(contentType),
	}

	downloadHtml, err := RenderFileDownload(data)
	if err != nil {
		log.Error("Error rendering file download HTML: ", err)
		return "", err
	}

	return string(downloadHtml), nil
}

// processTextUpload handles text content upload and returns the rendered HTML
func (p *PasteHandler) processTextUpload(req *http.Request, content, keyRaw, urlRaw string, metadata map[string]*string) (string, error) {
	syntax := req.Form.Get("syntax")
	if len(syntax) == 0 {
		syntax = "plaintext"
	}
	log.Debug("Using syntax: ", syntax)

	prePageData := PreData{
		LogoURL:     p.Config.LogoURL,
		IndexURL:    p.Config.IndexURL,
		FaviconURL:  p.Config.FaviconURL,
		Content:     "",
		DownloadURL: urlRaw,
	}

	// If content longer than 100 kilobytes, do not highlight it
	if len(content) > 1024*100 {
		log.Debugf("Content size more than 100kb: '%d' bytes, using pre tag", len(content))
		prePageData.Content = content
	} else {
		log.Debugf("Content size: '%d' bytes, highlighting it", len(content))
		highlightBuilder := strings.Builder{}
		if err := highlight(&highlightBuilder, content, syntax, p.Style); err != nil {
			log.Error("Error highlighting content: ", err)
			return "", err
		}
		prePageData.Content = highlightBuilder.String()
	}

	preHtmlPage, err := RenderOutputPre(prePageData)
	if err != nil {
		log.Error("Error rendering output pre HTML: ", err)
		return "", err
	}

	if err := p.Uploader.UploadString(req.Context(), keyRaw, content, contentTypeText, metadata); err != nil {
		log.Error("Error uploading raw content: ", err)
		return "", err
	}

	log.Info("Uploaded raw content with key: ", keyRaw)
	return string(preHtmlPage), nil
}

// getFormContent extracts content, file, and header from the form
func (p *PasteHandler) getFormContent(w http.ResponseWriter, req *http.Request) (string, multipart.File, *multipart.FileHeader, error) {
	content := req.Form.Get("content")
	file, header, err := req.FormFile("file")
	if err != nil && !errors.Is(err, http.ErrMissingFile) {
		log.Warn("Error retrieving the file: ", err)
		return "", nil, nil, err
	}

	if file != nil {
		defer file.Close()
	}

	if file == nil && len(content) == 0 {
		log.Info("Empty form content, redirecting to index")
		p.redirectToURL(w, req, "/")
		return "", nil, nil, ErrEmptyFormContent
	}

	return content, file, header, nil
}

// redirectToURL redirects the user to the specified URL.
func (p *PasteHandler) redirectToURL(w http.ResponseWriter, req *http.Request, urlStr string) {
	log.Infof("Redirecting to: %s", urlStr)
	http.Redirect(w, req, urlStr, http.StatusFound)
}

// RespondWithError sends an HTML error response with the given status code and message.
func (p *PasteHandler) RespondWithError(w http.ResponseWriter, statusCode int, message string, config *Config) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)

	data := ErrorData{
		StatusCode: statusCode,
		Message:    message,
		LogoURL:    config.LogoURL,
		IndexURL:   config.IndexURL,
		FaviconURL: config.FaviconURL,
	}

	html, err := renderPageWithData(string(errorHTML), data)
	if err != nil {
		log.Errorf("Error rendering error page: %v", err)
		_, err = w.Write([]byte("<h1>Internal Server Error</h1>"))
		if err != nil {
			log.Errorf("Failed to write error message: %v", err)
		}
		return
	}

	_, err = w.Write(html)
	if err != nil {
		log.Errorf("Error writing error response: %v", err)
	}
}
