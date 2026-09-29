package parser

import (
	"bufio"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"interlog/internal/models"
)

var (
	rePump       = regexp.MustCompile(`(?i)(?:bomba|idContrato\s*=\s*'?)\s*(\d+)`)
	reDispatch   = regexp.MustCompile(`CA_[a-f0-9-]{36}`)
	reDevice     = regexp.MustCompile(`(?i)(?:Equipo=|dispositivo=)?(CCTALK\[\d+\]|LCDM1000\[\d+\]|MEI-SCx\[\d+\]|CCTALK|LCDM1000|MEI-SCx)`)
	reException  = regexp.MustCompile(`(System\.[A-Za-z0-9\.]+Exception)`)
	reExParam    = regexp.MustCompile(`par[aá]metro con nombre:?\s*([A-Za-z0-9_]+)`)
	reAmount     = regexp.MustCompile(`\|(\d+\.\d{2})`)
)

func Latin1ToUTF8(latin1 []byte) string {
	isASCII := true
	for _, b := range latin1 {
		if b >= 128 {
			isASCII = false
			break
		}
	}
	if isASCII {
		return string(latin1)
	}

	runes := make([]rune, len(latin1))
	for i, b := range latin1 {
		runes[i] = rune(b)
	}
	return string(runes)
}

func isTimestampPrefix(b []byte) bool {
	if len(b) < 23 {
		return false
	}
	return b[4] == '-' && b[7] == '-' && b[10] == ' ' && b[13] == ':' && b[16] == ':' && b[19] == '.'
}

func ParseFile(filePath string) ([]*models.LogEntry, time.Duration, error) {
	t0 := time.Now()
	file, err := os.Open(filePath)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 1024*1024) // 1MB buffer

	var entries []*models.LogEntry
	// Pre-allocate slice estimate to avoid multiple reallocations
	entries = make([]*models.LogEntry, 0, 800000)

	var currentEntry *models.LogEntry
	lineNum := 0

	for {
		lineBytes, isPrefix, err := reader.ReadLine()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, 0, err
		}

		fullLine := lineBytes
		for isPrefix {
			var more []byte
			more, isPrefix, err = reader.ReadLine()
			if err != nil && err != io.EOF {
				return nil, 0, err
			}
			fullLine = append(fullLine, more...)
		}

		lineNum++

		if isTimestampPrefix(fullLine) {
			rawLine := Latin1ToUTF8(fullLine)
			entry := parseSingleLine(lineNum, rawLine)
			entries = append(entries, entry)
			currentEntry = entry
		} else if currentEntry != nil {
			// Multiline continuation (SQL query, stack trace, etc.)
			lineStr := Latin1ToUTF8(fullLine)
			currentEntry.Message += "\n" + lineStr
			// Check if multiline contains exception or error
			if !currentEntry.IsRealError && (strings.Contains(lineStr, "Exception") || strings.Contains(lineStr, "   en ")) {
				currentEntry.IsRealError = true
				if currentEntry.Category == "" || currentEntry.Category == models.CategoryRoutine {
					currentEntry.Category = models.CategoryException
				}
				if currentEntry.ErrorReason == "" {
					currentEntry.ErrorReason = "Exception Stack Trace"
				}
			}
		}
	}

	return entries, time.Since(t0), nil
}

func parseSingleLine(lineNum int, line string) *models.LogEntry {
	entry := &models.LogEntry{
		LineNumber: lineNum,
	}

	// 1. Timestamp (24 chars: "2026-05-15 00:00:00.0121")
	idx := 0
	if len(line) >= 24 && line[4] == '-' && line[7] == '-' && line[10] == ' ' && line[13] == ':' && line[16] == ':' && line[19] == '.' {
		// Find end of timestamp (first space after index 19)
		idx = 20
		for idx < len(line) && line[idx] != ' ' {
			idx++
		}
		entry.RawTime = line[:idx]
		t, err := time.Parse("2006-01-02 15:04:05.0000", entry.RawTime)
		if err == nil {
			entry.Timestamp = t
		}
	}

	// Skip spaces between timestamp and level
	for idx < len(line) && line[idx] == ' ' {
		idx++
	}

	// 2. Level (DEBUG, INFO, WARN, WARNING, ERROR, FATAL)
	levelStart := idx
	for idx < len(line) && line[idx] != ' ' {
		idx++
	}
	levelStr := line[levelStart:idx]
	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		entry.Level = models.LevelDebug
	case "INFO":
		entry.Level = models.LevelInfo
	case "WARN", "WARNING":
		entry.Level = models.LevelWarn
	case "ERROR", "FATAL":
		entry.Level = models.LevelError
	default:
		entry.Level = models.LevelInfo
	}

	// Skip spaces between level and component
	for idx < len(line) && line[idx] == ' ' {
		idx++
	}

	rest := line[idx:]

	// 3. Extract Component & Message
	parts := strings.SplitN(rest, " ", 2)
	if len(parts) > 0 {
		entry.Component = parts[0]
		if len(parts) > 1 {
			msg := parts[1]
			// Check for 32-char hex GUID immediately following component
			if len(msg) > 33 && isHexGUID(msg[:32]) && msg[32] == ' ' {
				entry.GUID = msg[:32]
				entry.Message = strings.TrimSpace(msg[33:])
			} else {
				entry.Message = strings.TrimSpace(msg)
			}
		}
	}

	classifyEntry(entry)
	return entry
}

func isHexGUID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for i := 0; i < 32; i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func classifyEntry(e *models.LogEntry) {
	msg := e.Message
	comp := e.Component
	fullText := comp + " " + msg

	// Fast pre-checks before regex execution
	if strings.Contains(fullText, "bomba") || strings.Contains(fullText, "Bomba") || strings.Contains(fullText, "idContrato") {
		if m := rePump.FindStringSubmatch(fullText); len(m) > 1 {
			e.PumpID = m[1]
		}
	}

	if strings.Contains(fullText, "CA_") {
		if m := reDispatch.FindString(fullText); m != "" {
			e.DispatchID = m
		}
	}

	if strings.Contains(fullText, "CCTALK") || strings.Contains(fullText, "LCDM") || strings.Contains(fullText, "MEI") {
		if m := reDevice.FindStringSubmatch(fullText); len(m) > 1 {
			e.Device = m[1]
		}
	}

	if strings.Contains(fullText, "Exception") {
		if m := reException.FindStringSubmatch(fullText); len(m) > 1 {
			e.ExceptionType = m[1]
		}
	}

	// Determine Noise
	if strings.Contains(comp, "ObtenerParametro") && strings.Contains(msg, "se asigna valor por defecto") {
		e.IsNoise = true
	} else if strings.Contains(comp, "Conectar") && strings.Contains(msg, "No es necesario restablecer") {
		e.IsNoise = true
	} else if strings.Contains(comp, "EvaluateQueryExecution") && strings.Contains(msg, "soporta m") {
		e.IsNoise = true
	} else if strings.Contains(comp, "transitionsTimer_Elapsed") || strings.Contains(comp, "LoadCurrentTransition") || strings.Contains(comp, "transitionsManager_PropertyChanged") {
		e.IsNoise = true
	} else if strings.Contains(comp, "StateMonitor.EvaluarHorarioDeAtencion") {
		e.IsNoise = true
	} else if strings.Contains(comp, "RunProcesses") {
		e.IsNoise = true
	}

	// Determine Real Errors (regardless of log level!)
	if e.Level == models.LevelError {
		e.IsRealError = true
		e.ErrorReason = "Reportado con nivel ERROR"
	} else if e.Level == models.LevelWarn {
		e.IsRealError = true
		e.ErrorReason = "Reportado con nivel WARN"
	}

	if strings.Contains(msg, "ERROR|") {
		e.IsRealError = true
		e.ErrorReason = "Servidor Gateway devolvió respuesta ERROR"
	} else if strings.Contains(msg, "Error en dispositivo=") {
		e.IsRealError = true
		e.ErrorReason = "Falla de dispositivo de hardware"
	} else if strings.Contains(msg, "No fue posible registrar la informacion de pago") {
		e.IsRealError = true
		e.ErrorReason = "Fallo registro de pago en servidor"
	} else if e.ExceptionType != "" {
		e.IsRealError = true
		e.ErrorReason = "Excepción del Sistema: " + e.ExceptionType
	}

	// Determine Category
	if e.DispatchID != "" || strings.Contains(fullText, "RegisterPaymentInformation") || strings.Contains(fullText, "RegistrarInfoPagoPendientes") || strings.Contains(fullText, "RegistrarPago") || strings.Contains(fullText, "ActualizarEnvioInfoPago") {
		e.Category = models.CategoryPayment
	} else if e.Device != "" || strings.Contains(fullText, "Cajero") || strings.Contains(fullText, "ErrorDispositivo") {
		e.Category = models.CategoryHardware
	} else if e.ExceptionType != "" || strings.Contains(fullText, "Ocurrió una excepción") {
		e.Category = models.CategoryException
	} else if e.PumpID != "" || strings.Contains(fullText, "GetOperationStatus") || strings.Contains(fullText, "GetStatusBomba") || strings.Contains(fullText, "CloseOpenOperation") || strings.Contains(fullText, "Preventa") {
		e.Category = models.CategoryPump
	} else if strings.Contains(fullText, "DatabaseService") || strings.Contains(fullText, "BDLocal") || strings.Contains(fullText, "ObtenerDatosDeConsulta") || strings.Contains(fullText, "EjecutarConsulta") {
		e.Category = models.CategoryDatabase
	} else if strings.Contains(fullText, "SocketCommunication") || strings.Contains(fullText, "TX:") || strings.Contains(fullText, "RX:") || strings.Contains(fullText, "gateway 127.0.0.1") {
		e.Category = models.CategorySocket
	} else {
		e.Category = models.CategoryRoutine
	}
}
