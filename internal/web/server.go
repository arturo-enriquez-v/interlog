package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"interlog/internal/ai"
	"interlog/internal/engine"
	"interlog/internal/manager"
	"interlog/internal/models"
)

//go:embed static/*
var staticFS embed.FS

type Server struct {
	mgr  *manager.LogManager
	port int
}

func NewServer(mgr *manager.LogManager, port int) *Server {
	return &Server{
		mgr:  mgr,
		port: port,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/files", s.handleFiles)
	mux.HandleFunc("/api/upload", s.handleUpload)
	mux.HandleFunc("/api/register_path", s.handleRegisterPath)
	mux.HandleFunc("/api/select", s.handleSelect)
	mux.HandleFunc("/api/summary", s.handleSummary)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/context", s.handleContext)
	mux.HandleFunc("/api/payments", s.handlePayments)
	mux.HandleFunc("/api/hardware", s.handleHardware)
	mux.HandleFunc("/api/exceptions", s.handleExceptions)
	mux.HandleFunc("/api/ai/extract", s.handleAIExtract)
	mux.HandleFunc("/api/ai/diagnose", s.handleAIDiagnose)
	mux.HandleFunc("/api/ai/status", s.handleAIStatus)

	// Static assets
	subFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		return err
	}
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
		ReadTimeout:  30 * time.Minute, // Generous timeout for 7GB+ streaming uploads
		WriteTimeout: 30 * time.Minute,
	}

	fmt.Printf("\n🚀 \033[1m\033[32mServidor Web activo en:\033[0m \033[1m\033[36mhttp://localhost:%d\033[0m\n", s.port)
	fmt.Printf("Motor optimizado para logs de hasta 7+ GB con consumo constante de memoria (RAM < 50MB).\n\n")

	return httpServer.ListenAndServe()
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodDelete {
		filename := r.URL.Query().Get("file")
		if filename == "" {
			http.Error(w, `{"error":"Falta parametro file"}`, http.StatusBadRequest)
			return
		}
		if err := s.mgr.DeleteFile(filename); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "deleted": filename})
		return
	}

	files := s.mgr.ListFiles()
	json.NewEncoder(w).Encode(files)
}

// Streaming upload with NO artificial limit (supports 7GB, 10GB or whatever disk permits)
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"Error al leer flujo multipart: %v"}`, err), http.StatusBadRequest)
		return
	}

	var meta *models.FileMeta

	for {
		part, err := mr.NextPart()
		if err != nil {
			if err == io.EOF {
				break
			}
			http.Error(w, fmt.Sprintf(`{"error":"Error al procesar flujo de archivo: %v"}`, err), http.StatusBadRequest)
			return
		}

		if part.FormName() == "log_file" {
			filename := part.FileName()
			if filename == "" {
				filename = "log_upload.log"
			}

			// Stream directly from HTTP body to disk (constant RAM)
			meta, err = s.mgr.SaveUploadedStream(filename, part)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
				return
			}
			break
		}
	}

	if meta == nil {
		http.Error(w, `{"error":"No se encontró el archivo 'log_file'"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"file":    meta,
	})
}

// Allows registering any huge 7GB file by path directly without uploading over HTTP
func (s *Server) handleRegisterPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		http.Error(w, `{"error":"Debe indicar el parámetro 'path'"}`, http.StatusBadRequest)
		return
	}

	meta, err := s.mgr.RegisterLocalPath(strings.TrimSpace(req.Path))
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"file":    meta,
	})
}

func (s *Server) handleSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fileName := r.URL.Query().Get("file")
	if fileName == "" {
		var body struct {
			FileName string `json:"file_name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		fileName = body.FileName
	}

	if fileName == "" {
		http.Error(w, `{"error":"Debe indicar el nombre del archivo"}`, http.StatusBadRequest)
		return
	}

	idxLog, err := s.mgr.SetActiveFile(fileName)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"summary": idxLog.Summary,
	})
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	idxLog, err := s.mgr.GetActiveIndexedLog()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(idxLog.Summary)
}

func (s *Server) handlePayments(w http.ResponseWriter, r *http.Request) {
	idxLog, err := s.mgr.GetActiveIndexedLog()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(idxLog.Summary.StuckPayments)
}

func (s *Server) handleHardware(w http.ResponseWriter, r *http.Request) {
	idxLog, err := s.mgr.GetActiveIndexedLog()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(idxLog.Summary.HardwareAlerts)
}

func (s *Server) handleExceptions(w http.ResponseWriter, r *http.Request) {
	idxLog, err := s.mgr.GetActiveIndexedLog()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(idxLog.Summary.Exceptions)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	idxLog, err := s.mgr.GetActiveIndexedLog()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	allRows := q.Get("all") == "true" || q.Get("all") == "1" || q.Get("all_rows") == "true"
	aroundLine, _ := strconv.Atoi(q.Get("around_line"))
	radius, _ := strconv.Atoi(q.Get("radius"))

	fromStr := q.Get("from")
	toStr := q.Get("to")
	fromTime, _ := engine.ParseFlexibleTime(fromStr)
	toTime, _ := engine.ParseFlexibleTime(toStr)

	params := models.FilterParams{
		Query:      q.Get("q"),
		Level:      models.LogLevel(q.Get("level")),
		Category:   models.Category(q.Get("category")),
		PumpID:     q.Get("pump"),
		DispatchID: q.Get("dispatch"),
		Device:     q.Get("device"),
		FromTime:   fromTime,
		ToTime:     toTime,
		RawFrom:    fromStr,
		RawTo:      toStr,
		HideNoise:  q.Get("hide_noise") == "true" || q.Get("hide_noise") == "1",
		OnlyErrors: q.Get("only_errors") == "true" || q.Get("only_errors") == "1",
		AllRows:    allRows,
		AroundLine: aroundLine,
		Radius:     radius,
		Limit:      limit,
		Offset:     offset,
	}

	result := idxLog.QueryLogs(params)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	idxLog, err := s.mgr.GetActiveIndexedLog()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()
	line, _ := strconv.Atoi(q.Get("line"))
	if line <= 0 {
		http.Error(w, `{"error":"parámetro 'line' inválido o faltante"}`, http.StatusBadRequest)
		return
	}
	radius, _ := strconv.Atoi(q.Get("radius"))
	if radius <= 0 {
		radius = 100
	}
	if radius > 1000 {
		radius = 1000
	}

	params := models.FilterParams{
		AroundLine: line,
		Radius:     radius,
	}

	result := idxLog.QueryLogs(params)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleAIExtract(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	idxLog, err := s.mgr.GetActiveIndexedLog()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	var req ai.ContextExtractRequest
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"petición JSON inválida: %v"}`, err), http.StatusBadRequest)
			return
		}
	} else {
		q := r.URL.Query()
		startLine, _ := strconv.Atoi(q.Get("start_line"))
		before, _ := strconv.Atoi(q.Get("lines_before"))
		after, _ := strconv.Atoi(q.Get("lines_after"))
		maxScan, _ := strconv.Atoi(q.Get("max_scan"))
		req = ai.ContextExtractRequest{
			Anchor:      q.Get("anchor"),
			StartLine:   startLine,
			LinesBefore: before,
			LinesAfter:  after,
			MaxScan:     maxScan,
			HideNoise:   q.Get("hide_noise") != "false",
		}
	}

	result, err := ai.ExtractContextDownwards(idxLog, req)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleAIDiagnose(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Método no permitido. Use POST"}`, http.StatusMethodNotAllowed)
		return
	}

	var req ai.DiagnosisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"JSON inválido: %v"}`, err), http.StatusBadRequest)
		return
	}

	if headerKey := r.Header.Get("X-Gemini-Key"); headerKey != "" && req.ApiKey == "" {
		req.ApiKey = headerKey
	}

	if strings.TrimSpace(req.ContextText) == "" {
		http.Error(w, `{"error":"El parámetro 'context_text' no puede estar vacío"}`, http.StatusBadRequest)
		return
	}

	resp, err := ai.CallGemini(req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	hasEnvKey := os.Getenv("GEMINI_API_KEY") != ""
	json.NewEncoder(w).Encode(map[string]interface{}{
		"has_env_key":   hasEnvKey,
		"default_model": "gemini-flash-lite-latest",
		"models": []string{
			"gemini-flash-lite-latest",
			"gemini-flash-latest",
			"gemini-pro-latest",
			"gemini-2.5-flash-lite",
		},
	})
}
