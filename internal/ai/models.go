package ai

import (
	"interlog/internal/models"
)

// ContextExtractRequest define los parámetros para la búsqueda de ancla y extracción de error hacia abajo
type ContextExtractRequest struct {
	Anchor      string `json:"anchor"`
	StartLine   int    `json:"start_line"`
	LinesBefore int    `json:"lines_before"`
	LinesAfter  int    `json:"lines_after"`
	MaxScan     int    `json:"max_scan"`
	HideNoise   bool   `json:"hide_noise"`
}

// ContextExtractResult es el resultado del algoritmo de corte de contexto
type ContextExtractResult struct {
	Success           bool               `json:"success"`
	Anchor            string             `json:"anchor"`
	AnchorFound       bool               `json:"anchor_found"`
	AnchorLine        int                `json:"anchor_line"`
	AnchorSnippet     string             `json:"anchor_snippet"`
	ErrorFound        bool               `json:"error_found"`
	ErrorLine         int                `json:"error_line"`
	ErrorDistance     int                `json:"error_distance"`
	ErrorReason       string             `json:"error_reason"`
	ErrorSnippet      string             `json:"error_snippet"`
	FocusLine         int                `json:"focus_line"`
	LinesBefore       int                `json:"lines_before"`
	LinesAfter        int                `json:"lines_after"`
	TotalContextLines int                `json:"total_context_lines"`
	ContextText       string             `json:"context_text"`
	EstimatedTokens   int                `json:"estimated_tokens"`
	Entries           []*models.LogEntry `json:"entries"`
	Message           string             `json:"message"`
}

// DiagnosisRequest define los parámetros para consultar a Gemini API
type DiagnosisRequest struct {
	ContextText  string `json:"context_text"`
	CustomPrompt string `json:"custom_prompt,omitempty"`
	ApiKey       string `json:"api_key,omitempty"`
	Model        string `json:"model,omitempty"`
}

// DiagnosisResponse contiene el análisis emitido por Gemini
type DiagnosisResponse struct {
	Success   bool   `json:"success"`
	Model     string `json:"model"`
	Analysis  string `json:"analysis"`
	Error     string `json:"error,omitempty"`
	LatencyMs int64  `json:"latency_ms"`
}
