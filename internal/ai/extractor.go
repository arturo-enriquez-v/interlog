package ai

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"interlog/internal/engine"
	"interlog/internal/models"
	"interlog/internal/parser"
)

// ExtractContextDownwards busca el ancla, desciende hasta encontrar un error y extrae el bloque de contexto
func ExtractContextDownwards(l *engine.IndexedLog, req ContextExtractRequest) (*ContextExtractResult, error) {
	if l == nil {
		return nil, fmt.Errorf("no hay archivo de log activo cargado")
	}

	linesBefore := req.LinesBefore
	if linesBefore <= 0 {
		linesBefore = 15
	}
	if linesBefore > 50 {
		linesBefore = 50
	}

	linesAfter := req.LinesAfter
	if linesAfter <= 0 {
		linesAfter = 15
	}
	if linesAfter > 50 {
		linesAfter = 50
	}

	maxScan := req.MaxScan
	if maxScan <= 0 {
		maxScan = 500
	}
	if maxScan > 3000 {
		maxScan = 3000
	}

	anchorStr := strings.TrimSpace(req.Anchor)
	anchorLine := req.StartLine
	anchorFound := false
	var anchorSnippet string

	// 1. Resolver Línea Ancla si no viene por StartLine
	if anchorLine <= 0 && anchorStr != "" {
		// ¿Es un número directo de línea? (ej: "349", "#349", "L349")
		cleanNum := strings.TrimPrefix(strings.TrimPrefix(anchorStr, "#"), "L")
		if num, err := strconv.Atoi(cleanNum); err == nil && num > 0 {
			anchorLine = num
			anchorFound = true
			anchorSnippet = fmt.Sprintf("Línea #%d solicitada directamente", num)
		} else {
			// Escanear para hallar la primera coincidencia del término ancla
			foundLine, snippet, err := findFirstAnchorLine(l, anchorStr)
			if err != nil {
				return nil, err
			}
			if foundLine > 0 {
				anchorLine = foundLine
				anchorFound = true
				anchorSnippet = snippet
			}
		}
	} else if anchorLine > 0 {
		anchorFound = true
		anchorSnippet = fmt.Sprintf("Iniciando desde línea #%d", anchorLine)
	} else {
		// Sin ancla ni línea, comenzar desde el inicio
		anchorLine = 1
		anchorFound = true
		anchorSnippet = "Inicio del archivo"
	}

	if !anchorFound {
		return &ContextExtractResult{
			Success:     false,
			Anchor:      anchorStr,
			AnchorFound: false,
			Message:     fmt.Sprintf("No se encontró ninguna coincidencia para el ancla '%s' en el log.", anchorStr),
		}, nil
	}

	// 2. Buscar hacia abajo desde anchorLine hasta hallar el primer error o agotar maxScan
	errorLine, errorReason, errorSnippet, errorFound, err := scanDownwardsForError(l, anchorLine, maxScan)
	if err != nil {
		return nil, fmt.Errorf("error al escanear hacia abajo: %w", err)
	}

	// 3. Determinar el foco del bloque de contexto
	focusLine := anchorLine
	errorDist := 0
	if errorFound {
		focusLine = errorLine
		errorDist = errorLine - anchorLine
	}

	// 4. Extraer el rango de líneas alrededor del foco
	startLine := focusLine - linesBefore
	if startLine < 1 {
		startLine = 1
	}
	endLine := focusLine + linesAfter
	if l.TotalLines > 0 && endLine > l.TotalLines {
		endLine = l.TotalLines
	}

	rawEntries, err := l.GetLinesRange(startLine, endLine)
	if err != nil {
		return nil, fmt.Errorf("error al obtener líneas de contexto: %w", err)
	}

	// 5. Filtrar ruido si está solicitado (pero preservando ancla y error siempre)
	var filteredEntries []*models.LogEntry
	for _, e := range rawEntries {
		if req.HideNoise && e.IsNoise && e.LineNumber != anchorLine && e.LineNumber != errorLine {
			continue
		}
		filteredEntries = append(filteredEntries, e)
	}

	// 6. Construir texto estructurado para el prompt de IA y visor
	var sb strings.Builder
	sb.WriteString("=== EXTRACTO DE TELEMETRÍA DE LOG (QUICKPAY / CAJERO) ===\n")
	sb.WriteString(fmt.Sprintf("Archivo: %s\n", l.FileName))
	sb.WriteString(fmt.Sprintf("Parámetro de Búsqueda (Ancla): \"%s\" (Línea #%d)\n", anchorStr, anchorLine))
	if errorFound {
		sb.WriteString(fmt.Sprintf("Primer Error Encontrado: Línea #%d (a %d líneas del ancla)\n", errorLine, errorDist))
		sb.WriteString(fmt.Sprintf("Diagnóstico Inicial / Motivo: %s\n", errorReason))
	} else {
		sb.WriteString(fmt.Sprintf("Estado: No se encontró un error en las %d líneas posteriores. Mostrando contexto del ancla.\n", maxScan))
	}
	sb.WriteString(fmt.Sprintf("Líneas mostradas: %d a %d (Total en extracto: %d líneas)\n", startLine, endLine, len(filteredEntries)))
	sb.WriteString("----------------------------------------------------------\n\n")

	for _, e := range filteredEntries {
		marker := "   "
		if e.LineNumber == anchorLine {
			marker = "📍 [ANCLA] >>>"
		}
		if errorFound && e.LineNumber == errorLine {
			marker = "🚨 [ERROR] >>>"
		}

		lvl := fmt.Sprintf("%-5s", e.Level)
		sb.WriteString(fmt.Sprintf("%s [L%-6d] [%s] %s %s: %s\n",
			marker, e.LineNumber, e.RawTime, lvl, e.Component, e.Message))
	}
	sb.WriteString("\n==========================================================")

	contextStr := sb.String()
	tokenEstimate := int(float64(len(contextStr)) / 3.8)

	msg := "Contexto y error extraídos exitosamente."
	if !errorFound {
		msg = fmt.Sprintf("Se encontró el ancla en la línea #%d, pero no hubo errores explícitos en las %d líneas siguientes.", anchorLine, maxScan)
	}

	return &ContextExtractResult{
		Success:           true,
		Anchor:            anchorStr,
		AnchorFound:       true,
		AnchorLine:        anchorLine,
		AnchorSnippet:     anchorSnippet,
		ErrorFound:        errorFound,
		ErrorLine:         errorLine,
		ErrorDistance:     errorDist,
		ErrorReason:       errorReason,
		ErrorSnippet:      errorSnippet,
		FocusLine:         focusLine,
		LinesBefore:       linesBefore,
		LinesAfter:        linesAfter,
		TotalContextLines: len(filteredEntries),
		ContextText:       contextStr,
		EstimatedTokens:   tokenEstimate,
		Entries:           filteredEntries,
		Message:           msg,
	}, nil
}

// findFirstAnchorLine busca secuencialmente la primera ocurrencia del ancla
func findFirstAnchorLine(l *engine.IndexedLog, anchor string) (int, string, error) {
	file, err := os.Open(l.FilePath)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 512*1024)
	queryLower := strings.ToLower(anchor)
	currentLine := 0

	for {
		lineBytes, err := reader.ReadBytes('\n')
		if err != nil && len(lineBytes) == 0 {
			break
		}
		currentLine++
		fullLine := bytes.TrimRight(lineBytes, "\r\n")

		// Búsqueda rápida de subcadena en bytes antes de decodificar UTF-8
		if bytes.Contains(bytes.ToLower(fullLine), []byte(queryLower)) {
			rawLine := parser.Latin1ToUTF8(fullLine)
			entry := engine.ParseSingleLine(currentLine, rawLine)
			snippet := fmt.Sprintf("[%s] %s: %s", entry.RawTime, entry.Component, entry.Message)
			return currentLine, snippet, nil
		}
	}
	return 0, "", nil
}

// scanDownwardsForError desciende desde startLine hasta startLine+maxScan buscando la primera línea de error
func scanDownwardsForError(l *engine.IndexedLog, startLine, maxScan int) (int, string, string, bool, error) {
	file, err := os.Open(l.FilePath)
	if err != nil {
		return 0, "", "", false, err
	}
	defer file.Close()

	reader, currentLine, err := l.SeekToLine(file, startLine)
	if err != nil {
		return 0, "", "", false, err
	}

	targetEnd := startLine + maxScan
	if l.TotalLines > 0 && targetEnd > l.TotalLines {
		targetEnd = l.TotalLines
	}

	for {
		lineBytes, err := reader.ReadBytes('\n')
		if err != nil && len(lineBytes) == 0 {
			break
		}
		currentLine++

		if currentLine < startLine {
			continue
		}
		if currentLine > targetEnd {
			break
		}

		fullLine := bytes.TrimRight(lineBytes, "\r\n")
		rawLine := parser.Latin1ToUTF8(fullLine)
		entry := engine.ParseSingleLine(currentLine, rawLine)

		// Criterio de error real
		isErr := false
		msgLower := strings.ToLower(entry.Message)

		if entry.IsRealError || entry.Level == models.LevelError || entry.Level == models.LevelWarn {
			isErr = true
		} else if entry.ExceptionType != "" {
			isErr = true
		} else if strings.Contains(msgLower, "error") || strings.Contains(msgLower, "fallo") || strings.Contains(msgLower, "falla") {
			// Evitar falsos positivos como "Error=False" o "Error=0"
			if !strings.Contains(msgLower, "error=false") && !strings.Contains(msgLower, "error=0") && !strings.Contains(msgLower, "error=none") {
				isErr = true
			}
		}

		// Si estamos en la misma línea ancla, solo considerarla error si es un error formal de nivel ERROR o excepción
		if currentLine == startLine && !entry.IsRealError && entry.Level != models.LevelError {
			isErr = false
		}

		if isErr {
			reason := entry.ErrorReason
			if reason == "" {
				if entry.Level == models.LevelError {
					reason = "Registro reportado con nivel ERROR"
				} else if entry.ExceptionType != "" {
					reason = "Excepción: " + entry.ExceptionType
				} else {
					reason = "Anomalía o fallo detectado en mensaje"
				}
			}
			return currentLine, reason, entry.Message, true, nil
		}
	}

	return 0, "", "", false, nil
}
