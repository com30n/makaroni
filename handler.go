package makaroni

import (
	"fmt"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strings"
)

var contentTypeHTML = "text/html"
var contentTypeText = "text/plain"

type PasteHandler struct {
	IndexHTML          []byte
	OutputHTMLPre      []byte
	Upload             func(key string, content string, contentType string) error
	Style              string
	ResultURLPrefix    string
	MultipartMaxMemory int64
}

// RespondServerInternalError sends a response with status 500 and logs the error.
func RespondServerInternalError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	log.Error(err)
}

// ServeHTTP handles HTTP requests using different log levels.
func (p *PasteHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	log.Debug("Received request: ", req.Method, " ", req.URL.Path)

	if req.Method == http.MethodGet {
		log.Info("Sending index page")
		w.Header().Set("Content-Type", contentTypeHTML)
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(p.IndexHTML); err != nil {
			log.Error("Error sending indexHTML: ", err)
		}
		return
	}

	if req.Method != http.MethodPost {
		log.Warn("Unsupported request method: ", req.Method)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if err := req.ParseMultipartForm(p.MultipartMaxMemory); err != nil {
		log.Warn("Error parsing form: ", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	content := req.Form.Get("content")
	if len(content) == 0 {
		log.Warn("Empty form content")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	syntax := req.Form.Get("syntax")
	if len(syntax) == 0 {
		syntax = "plaintext"
	}

	uuidV4, err := uuid.NewRandom()
	if err != nil {
		RespondServerInternalError(w, err)
		return
	}
	keyRaw := uuidV4.String()
	keyHTML := keyRaw + ".html"
	urlHTML := p.ResultURLPrefix + keyHTML
	urlRaw := p.ResultURLPrefix + keyRaw

	builder := strings.Builder{}
	// todo: use a better templating approach
	builder.Write(p.OutputHTMLPre)
	builder.Write([]byte(fmt.Sprintf("<div class=\"nav\"><a href=\"%s\">raw</a></div>", urlRaw)))

	if err := highlight(&builder, content, syntax, p.Style); err != nil {
		RespondServerInternalError(w, err)
		return
	}
	html := builder.String()

	if err := p.Upload(keyRaw, content, contentTypeText); err != nil {
		RespondServerInternalError(w, err)
		return
	}
	log.Debug("Uploaded raw content with key: ", keyRaw)

	if err := p.Upload(keyHTML, html, contentTypeHTML); err != nil {
		RespondServerInternalError(w, err)
		return
	}
	log.Info("Uploaded HTML content with key: ", keyHTML)

	w.Header().Set("Location", urlHTML)
	w.WriteHeader(http.StatusFound)
	log.Debug("Redirecting to URL: ", urlHTML)
}
