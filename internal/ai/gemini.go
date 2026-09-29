package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role,omitempty"`
}

type geminiRequestBody struct {
	Contents          []geminiContent `json:"contents"`
	GenerationConfig  map[string]any  `json:"generationConfig,omitempty"`
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
}

type geminiResponseCandidate struct {
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"content"`
	FinishReason string `json:"finishReason"`
}

type geminiAPIResponse struct {
	Candidates []geminiResponseCandidate `json:"candidates"`
	Error      *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

// CallGemini envía el contexto extraído a Google AI Studio (Gemini)
func CallGemini(req DiagnosisRequest) (*DiagnosisResponse, error) {
	t0 := time.Now()

	apiKey := strings.TrimSpace(req.ApiKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no se proporcionó API Key de Google AI Studio. Ingresa tu API Key en la pantalla o define la variable de entorno GEMINI_API_KEY")
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "gemini-flash-latest"
	}

	// Construir prompt especializado
	var promptBuilder strings.Builder
	promptBuilder.WriteString("Eres un Ingeniero Principal de Diagnóstico y Soporte para sistemas de Cajeros Automáticos y Estaciones de Servicio (QuickPay / Interlogic).\n")
	promptBuilder.WriteString("Tu objetivo es analizar el extracto de log y explicar de manera clara, concisa y profesional qué ocurrió y cómo corregirlo.\n\n")

	if req.CustomPrompt != "" {
		promptBuilder.WriteString("PREGUNTA O INSTRUCCIÓN ESPECÍFICA DEL USUARIO:\n")
		promptBuilder.WriteString(req.CustomPrompt)
		promptBuilder.WriteString("\n\n")
	}

	promptBuilder.WriteString("FORMATO DE RESPUESTA EN MARKDOWN:\n")
	promptBuilder.WriteString("### 1. Diagnóstico Rápido\n(Explica en 1 o 2 oraciones qué falló y qué componente está involucrado)\n\n")
	promptBuilder.WriteString("### 2. Análisis de Causa Raíz\n(Analiza la secuencia de eventos cronológica previa a la falla que detonó el problema)\n\n")
	promptBuilder.WriteString("### 3. Impacto en la Operación\n(¿Afecta pagos, dispensación de billetes, comunicación socket o venta de combustible?)\n\n")
	promptBuilder.WriteString("### 4. Acciones Recomendadas\n(Pasos prácticos específicos para resolverlo: hardware, configuración, red o código)\n\n")
	promptBuilder.WriteString("EXTRACTO DEL LOG:\n```text\n")
	promptBuilder.WriteString(req.ContextText)
	promptBuilder.WriteString("\n```\n")

	bodyObj := geminiRequestBody{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: promptBuilder.String()},
				},
			},
		},
		GenerationConfig: map[string]any{
			"temperature":     0.2,
			"maxOutputTokens": 2048,
		},
	}

	bodyBytes, err := json.Marshal(bodyObj)
	if err != nil {
		return nil, fmt.Errorf("error al serializar petición a Gemini: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("error al crear solicitud HTTP: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-goog-api-key", apiKey)

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("error de conexión con Google Gemini API: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error al leer respuesta de Gemini: %w", err)
	}

	var apiResp geminiAPIResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("error al decodificar respuesta JSON de Gemini: %w", err)
	}

	if apiResp.Error != nil {
		// Fallback automático al modelo lite estable ante picos de demanda (503) o modelo no disponible (404)
		if (apiResp.Error.Code == 503 || apiResp.Error.Code == 404) && model != "gemini-flash-lite-latest" {
			reqFallback := req
			reqFallback.Model = "gemini-flash-lite-latest"
			return CallGemini(reqFallback)
		}
		return nil, fmt.Errorf("Google Gemini API error [%d %s]: %s", apiResp.Error.Code, apiResp.Error.Status, apiResp.Error.Message)
	}

	if len(apiResp.Candidates) == 0 || len(apiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("Gemini no generó ninguna respuesta para este extracto")
	}

	var fullAnalysis strings.Builder
	for _, part := range apiResp.Candidates[0].Content.Parts {
		if part.Text != "" {
			fullAnalysis.WriteString(part.Text)
		}
	}
	analysisText := fullAnalysis.String()
	if analysisText == "" {
		return nil, fmt.Errorf("la respuesta de Gemini no contuvo texto legible")
	}

	latency := time.Since(t0).Milliseconds()

	return &DiagnosisResponse{
		Success:   true,
		Model:     model,
		Analysis:  analysisText,
		LatencyMs: latency,
	}, nil
}
