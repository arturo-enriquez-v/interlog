package engine

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"interlog/internal/models"
	"interlog/internal/parser"
)

const CheckpointInterval = 500 // Save byte offset every 500 lines for O(1) seeking

var (
	reHWDevice = regexp.MustCompile(`(?:Equipo=|dispositivo=)([^,\s]+)`)
	reHWCode   = regexp.MustCompile(`Codigo=([^,\s]+)`)
	reHWDesc   = regexp.MustCompile(`Descripcion=([^,\n]+)`)
	reAmount   = regexp.MustCompile(`\|(\d+\.\d{2})`)
	reParamEx  = regexp.MustCompile(`par[aá]metro con nombre:?\s*([A-Za-z0-9_]+)`)
)

type IndexedLog struct {
	mu              sync.RWMutex
	FilePath        string
	FileName        string
	FileSizeMB      float64
	TotalLines      int
	Checkpoints     []int64     // Byte offsets of lines 0, 500, 1000, 1500...
	CheckpointTimes []time.Time // Timestamps of lines 0, 500, 1000, 1500...
	RealErrors      []*models.LogEntry
	Summary         *models.Summary
	ParseTime       time.Duration
}

func ParseFlexibleTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	formats := []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05.0000",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("formato no reconocido: %s", s)
}

func IndexFile(filePath string) (*IndexedLog, error) {
	t0 := time.Now()
	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("error al leer archivo '%s': %w", filePath, err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error al abrir archivo: %w", err)
	}
	defer file.Close()

	sizeMB := float64(fi.Size()) / (1024 * 1024)
	reader := bufio.NewReaderSize(file, 2*1024*1024) // 2MB read buffer

	idxLog := &IndexedLog{
		FilePath:        filePath,
		FileName:        filepath.Base(filePath),
		FileSizeMB:      sizeMB,
		Checkpoints:     make([]int64, 0, 10000),
		CheckpointTimes: make([]time.Time, 0, 10000),
		RealErrors:      make([]*models.LogEntry, 0, 50000),
		Summary: &models.Summary{
			FilePath:        filePath,
			FileSizeMB:      sizeMB,
			LevelCounts:     make(map[string]int),
			CategoriesCount: make(map[string]int),
		},
	}

	// Aggregators for streaming analysis
	paymentsMap := make(map[string]*models.StuckPayment)
	hwAlertsMap := make(map[string]models.HardwareAlert)
	exMap := make(map[string]*models.ExceptionSummary)
	pumpMap := make(map[string]*models.PumpSummary)
	noiseMap := make(map[string]int)

	var currentOffset int64 = 0
	lineNum := 0
	var firstTimestamp, lastTimestamp string
	var firstTime, lastTime time.Time
	var currentEntry *models.LogEntry

	for {
		lineOffset := currentOffset
		lineBytes, err := reader.ReadBytes('\n')
		if err != nil && len(lineBytes) == 0 {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		currentOffset += int64(len(lineBytes))
		fullLine := bytes.TrimRight(lineBytes, "\r\n")

		lineNum++

		// Fast check for timestamp prefix: "2026-05-15 00:00:00.0121"
		if len(fullLine) >= 24 && fullLine[4] == '-' && fullLine[7] == '-' && fullLine[10] == ' ' && fullLine[13] == ':' && fullLine[16] == ':' && fullLine[19] == '.' {
			rawLine := parser.Latin1ToUTF8(fullLine)
			entry := parseSingleLine(lineNum, rawLine)
			currentEntry = entry

			// Save checkpoint every CheckpointInterval lines with timestamp
			if (lineNum-1)%CheckpointInterval == 0 {
				idxLog.Checkpoints = append(idxLog.Checkpoints, lineOffset)
				idxLog.CheckpointTimes = append(idxLog.CheckpointTimes, entry.Timestamp)
			}

			if firstTimestamp == "" {
				firstTimestamp = entry.RawTime
				firstTime = entry.Timestamp
			}
			lastTimestamp = entry.RawTime
			if !entry.Timestamp.IsZero() {
				lastTime = entry.Timestamp
			}

			// Aggregate counters
			idxLog.Summary.LevelCounts[string(entry.Level)]++
			idxLog.Summary.CategoriesCount[string(entry.Category)]++

			if entry.IsNoise {
				idxLog.Summary.NoiseCount++
				if strings.Contains(entry.Component, "ObtenerParametro") {
					noiseMap["Parametros no encontrados (default)"]++
				} else if strings.Contains(entry.Component, "Conectar") {
					noiseMap["Verificación conexión BD abierta"]++
				} else if strings.Contains(entry.Component, "EvaluateQueryExecution") {
					noiseMap["Evaluación Resultsets simultáneos"]++
				} else if strings.Contains(entry.Component, "transitions") || strings.Contains(entry.Component, "Transition") {
					noiseMap["Transiciones pantalla de Bienvenida"]++
				} else {
					noiseMap["Otras rutinas de sondeo"]++
				}
			}

			if entry.IsRealError {
				idxLog.Summary.RealErrorsCount++
				// Keep errors up to 100,000 in memory to guard RAM on 7GB+ files
				if len(idxLog.RealErrors) < 100000 {
					idxLog.RealErrors = append(idxLog.RealErrors, entry)
				}
			}

			// 1. Stuck Payments tracking
			if entry.DispatchID != "" {
				sp, exists := paymentsMap[entry.DispatchID]
				if !exists {
					sp = &models.StuckPayment{
						DispatchID: entry.DispatchID,
						FirstSeen:  entry.RawTime,
						LastError:  "Pending registration",
					}
					paymentsMap[entry.DispatchID] = sp
				}
				sp.LastSeen = entry.RawTime
				if strings.Contains(entry.Message, "TX: 10||") || strings.Contains(entry.Component, "RegisterPaymentInformation") {
					sp.Retries++
				}
				if sp.Amount == "" {
					if m := reAmount.FindStringSubmatch(entry.Message); len(m) > 1 {
						sp.Amount = "$" + m[1]
					}
				}
				if strings.Contains(entry.Message, "ERROR|. Metodo RegistrarPago") {
					sp.LastError = "ERROR: Servidor rechazó RegistrarPago"
				}
			}

			// 2. Hardware Alerts tracking
			if strings.Contains(entry.Message, "Error en dispositivo=") || strings.Contains(entry.Message, "ErrorDispositivo") {
				dev := ""
				if m := reHWDevice.FindStringSubmatch(entry.Message); len(m) > 1 {
					dev = m[1]
				} else if entry.Device != "" {
					dev = entry.Device
				}
				code := ""
				if m := reHWCode.FindStringSubmatch(entry.Message); len(m) > 1 {
					code = m[1]
				}
				desc := ""
				if m := reHWDesc.FindStringSubmatch(entry.Message); len(m) > 1 {
					desc = m[1]
				} else {
					desc = entry.Message
				}

				key := fmt.Sprintf("%s:%s:%s", dev, code, desc)
				if _, exists := hwAlertsMap[key]; !exists {
					hwAlertsMap[key] = models.HardwareAlert{
						Device:      dev,
						ErrorCode:   code,
						Description: desc,
						Timestamp:   entry.RawTime,
						LineNumber:  entry.LineNumber,
					}
				}
			}

			// 3. Exception breakdown
			if entry.ExceptionType != "" {
				param := ""
				if m := reParamEx.FindStringSubmatch(entry.Message); len(m) > 1 {
					param = m[1]
				}
				key := entry.ExceptionType
				if param != "" {
					key = fmt.Sprintf("%s (%s)", entry.ExceptionType, param)
				}
				exStat, exists := exMap[key]
				if !exists {
					exStat = &models.ExceptionSummary{
						Type:       entry.ExceptionType,
						ParamName:  param,
						SampleLine: entry.LineNumber,
						SampleMsg:  entry.Message,
					}
					exMap[key] = exStat
				}
				exStat.Count++
			}

			// 4. Pump tracking
			if entry.PumpID != "" {
				p, exists := pumpMap[entry.PumpID]
				if !exists {
					p = &models.PumpSummary{
						PumpID: entry.PumpID,
					}
					pumpMap[entry.PumpID] = p
				}
				p.Mentions++
				if strings.Contains(entry.Message, "Estado de la bomba") {
					parts := strings.Split(entry.Message, ": ")
					if len(parts) > 1 {
						p.LastState = strings.TrimSpace(parts[1])
					}
				}
				if strings.Contains(entry.Message, "Existen") && strings.Contains(entry.Message, "operaciones abiertas") {
					p.OpenOpsFound++
				}
				if strings.Contains(entry.Component, "CerrarOperacionFlow") {
					p.ClosedOpsCount++
				}
			}
		} else if currentEntry != nil {
			// Multiline entry continuation
			lineStr := parser.Latin1ToUTF8(fullLine)
			if len(currentEntry.Message) < 4000 {
				currentEntry.Message += "\n" + lineStr
			}
		}
	}

	parseDur := time.Since(t0)
	idxLog.ParseTime = parseDur
	idxLog.TotalLines = lineNum

	s := idxLog.Summary
	s.TotalLines = lineNum
	s.StartTime = firstTimestamp
	s.EndTime = lastTimestamp
	s.ParseTimeMs = parseDur.Milliseconds()

	if !firstTime.IsZero() && !lastTime.IsZero() {
		dur := lastTime.Sub(firstTime)
		hours := int(dur.Hours())
		mins := int(dur.Minutes()) % 60
		secs := int(dur.Seconds()) % 60
		s.Duration = fmt.Sprintf("%02dh %02dm %02ds", hours, mins, secs)
	}

	if s.TotalLines > 0 {
		s.NoisePercent = float64(s.NoiseCount) / float64(s.TotalLines) * 100.0
	}

	// Format Stuck Payments list
	for _, p := range paymentsMap {
		if p.Retries == 0 {
			p.Retries = 1
		}
		s.StuckPayments = append(s.StuckPayments, *p)
	}
	sort.Slice(s.StuckPayments, func(i, j int) bool {
		return s.StuckPayments[i].Retries > s.StuckPayments[j].Retries
	})

	// Format Hardware Alerts list
	for _, h := range hwAlertsMap {
		s.HardwareAlerts = append(s.HardwareAlerts, h)
	}
	sort.Slice(s.HardwareAlerts, func(i, j int) bool {
		return s.HardwareAlerts[i].LineNumber < s.HardwareAlerts[j].LineNumber
	})

	// Format Exceptions list
	for _, ex := range exMap {
		s.Exceptions = append(s.Exceptions, *ex)
	}
	sort.Slice(s.Exceptions, func(i, j int) bool {
		return s.Exceptions[i].Count > s.Exceptions[j].Count
	})

	// Format Pumps list
	for _, p := range pumpMap {
		s.Pumps = append(s.Pumps, *p)
	}
	sort.Slice(s.Pumps, func(i, j int) bool {
		return s.Pumps[i].Mentions > s.Pumps[j].Mentions
	})

	// Format Noise patterns
	for pat, count := range noiseMap {
		pct := 0.0
		if s.TotalLines > 0 {
			pct = float64(count) / float64(s.TotalLines) * 100.0
		}
		s.NoiseBreakdown = append(s.NoiseBreakdown, models.NoiseStat{
			Pattern: pat,
			Count:   count,
			Percent: pct,
		})
	}
	sort.Slice(s.NoiseBreakdown, func(i, j int) bool {
		return s.NoiseBreakdown[i].Count > s.NoiseBreakdown[j].Count
	})

	return idxLog, nil
}

func (l *IndexedLog) QueryLogs(p models.FilterParams) models.FilterResult {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Case 0: Linear Context around a specific line number
	if p.AroundLine > 0 {
		radius := p.Radius
		if radius <= 0 {
			radius = 100
		}
		if radius > 1000 {
			radius = 1000
		}
		startLine := p.AroundLine - radius
		if startLine < 1 {
			startLine = 1
		}
		endLine := p.AroundLine + radius
		if endLine > l.TotalLines {
			endLine = l.TotalLines
		}
		count := (endLine - startLine) + 1
		offset := startLine - 1 // 0-indexed

		file, err := os.Open(l.FilePath)
		if err != nil {
			return models.FilterResult{Limit: count, Offset: offset}
		}
		defer file.Close()

		res := l.readSequentialSlice(file, offset, count)
		res.TotalFiltered = l.TotalLines
		return res
	}

	// Case 1: Fast filtering over in-memory RealErrors when only_errors is requested
	if p.OnlyErrors {
		return filterSlice(l.RealErrors, p)
	}

	// Case 2: On-demand streaming from disk using checkpoints or streaming scan
	limit := p.Limit
	if p.AllRows {
		if limit <= 0 || limit > 10000 {
			limit = 10000
		}
	} else {
		if limit <= 0 || limit > 500 {
			limit = 100
		}
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}

	file, err := os.Open(l.FilePath)
	if err != nil {
		return models.FilterResult{Limit: limit, Offset: offset}
	}
	defer file.Close()

	// Check if this is a simple raw view (no query, no noise filter, no category filter, no time filters)
	if p.Query == "" && !p.HideNoise && p.Level == "" && p.Category == "" && p.PumpID == "" && p.DispatchID == "" && p.Device == "" && p.FromTime.IsZero() && p.ToTime.IsZero() {
		return l.readSequentialSlice(file, offset, limit)
	}

	// Case 3: Filtered streaming search across file
	return l.streamFiltered(file, p, offset, limit)
}

func (l *IndexedLog) readSequentialSlice(file *os.File, offset, limit int) models.FilterResult {
	if offset >= l.TotalLines {
		return models.FilterResult{TotalFiltered: l.TotalLines, Limit: limit, Offset: offset, Entries: []*models.LogEntry{}}
	}

	// Jump directly using checkpoint
	cpIdx := offset / CheckpointInterval
	if cpIdx < len(l.Checkpoints) {
		file.Seek(l.Checkpoints[cpIdx], io.SeekStart)
	}

	reader := bufio.NewReaderSize(file, 512*1024)
	currentLine := cpIdx * CheckpointInterval
	var entries []*models.LogEntry

	for {
		lineBytes, err := reader.ReadBytes('\n')
		if err != nil && len(lineBytes) == 0 {
			break
		}
		currentLine++
		fullLine := bytes.TrimRight(lineBytes, "\r\n")

		if currentLine > offset {
			rawLine := parser.Latin1ToUTF8(fullLine)
			entry := parseSingleLine(currentLine, rawLine)
			entries = append(entries, entry)
			if len(entries) >= limit {
				break
			}
		}
	}

	return models.FilterResult{
		TotalFiltered: l.TotalLines,
		Limit:         limit,
		Offset:        offset,
		Entries:       entries,
	}
}

func (l *IndexedLog) streamFiltered(file *os.File, p models.FilterParams, offset, limit int) models.FilterResult {
	queryLower := strings.ToLower(strings.TrimSpace(p.Query))

	// Fast seek using CheckpointTimes if FromTime is specified!
	startLineNum := 0
	if !p.FromTime.IsZero() && len(l.CheckpointTimes) > 0 {
		bestIdx := 0
		for i, cpTime := range l.CheckpointTimes {
			if !cpTime.IsZero() && !cpTime.After(p.FromTime) {
				bestIdx = i
			} else if !cpTime.IsZero() && cpTime.After(p.FromTime) {
				break
			}
		}
		if bestIdx < len(l.Checkpoints) {
			file.Seek(l.Checkpoints[bestIdx], io.SeekStart)
			startLineNum = bestIdx * CheckpointInterval
		}
	}

	reader := bufio.NewReaderSize(file, 1024*1024)
	var matched []*models.LogEntry
	lineNum := startLineNum
	matchCount := 0

	for {
		lineBytes, err := reader.ReadBytes('\n')
		if err != nil && len(lineBytes) == 0 {
			break
		}
		lineNum++
		fullLine := bytes.TrimRight(lineBytes, "\r\n")

		// Only parse lines starting with timestamp
		if len(fullLine) < 24 || fullLine[4] != '-' || fullLine[7] != '-' || fullLine[10] != ' ' {
			continue
		}

		rawLine := parser.Latin1ToUTF8(fullLine)
		entry := parseSingleLine(lineNum, rawLine)

		// Filter by Date/Time Interval
		if !p.FromTime.IsZero() && !entry.Timestamp.IsZero() && entry.Timestamp.Before(p.FromTime) {
			continue
		}
		if !p.ToTime.IsZero() && !entry.Timestamp.IsZero() && entry.Timestamp.After(p.ToTime) {
			// Chronological order: all subsequent lines are also beyond ToTime!
			break
		}

		if p.HideNoise && entry.IsNoise {
			continue
		}
		if p.Level != "" && entry.Level != p.Level {
			continue
		}
		if p.Category != "" && entry.Category != p.Category {
			continue
		}
		if p.PumpID != "" && entry.PumpID != p.PumpID {
			continue
		}
		if p.DispatchID != "" && entry.DispatchID != p.DispatchID {
			continue
		}
		if p.Device != "" && !strings.EqualFold(entry.Device, p.Device) {
			continue
		}

		if queryLower != "" {
			inComp := strings.Contains(strings.ToLower(entry.Component), queryLower)
			inMsg := strings.Contains(strings.ToLower(entry.Message), queryLower)
			inGUID := strings.Contains(strings.ToLower(entry.GUID), queryLower)
			inDisp := strings.Contains(strings.ToLower(entry.DispatchID), queryLower)
			if !inComp && !inMsg && !inGUID && !inDisp {
				continue
			}
		}

		matchCount++
		if matchCount > offset && len(matched) < limit {
			matched = append(matched, entry)
		}

		// Fast early break if we have enough matches
		if len(matched) >= limit && (!p.AllRows || matchCount > offset+1000) {
			break
		}
	}

	return models.FilterResult{
		TotalFiltered: matchCount,
		Limit:         limit,
		Offset:        offset,
		Entries:       matched,
	}
}

func filterSlice(entries []*models.LogEntry, p models.FilterParams) models.FilterResult {
	queryLower := strings.ToLower(strings.TrimSpace(p.Query))
	var matched []*models.LogEntry

	for _, e := range entries {
		// Filter by Date/Time Interval
		if !p.FromTime.IsZero() && !e.Timestamp.IsZero() && e.Timestamp.Before(p.FromTime) {
			continue
		}
		if !p.ToTime.IsZero() && !e.Timestamp.IsZero() && e.Timestamp.After(p.ToTime) {
			continue
		}

		if p.HideNoise && e.IsNoise {
			continue
		}
		if p.Level != "" && e.Level != p.Level {
			continue
		}
		if p.Category != "" && e.Category != p.Category {
			continue
		}
		if p.PumpID != "" && e.PumpID != p.PumpID {
			continue
		}
		if p.DispatchID != "" && e.DispatchID != p.DispatchID {
			continue
		}
		if p.Device != "" && !strings.EqualFold(e.Device, p.Device) {
			continue
		}

		if queryLower != "" {
			inComp := strings.Contains(strings.ToLower(e.Component), queryLower)
			inMsg := strings.Contains(strings.ToLower(e.Message), queryLower)
			inGUID := strings.Contains(strings.ToLower(e.GUID), queryLower)
			inDisp := strings.Contains(strings.ToLower(e.DispatchID), queryLower)
			if !inComp && !inMsg && !inGUID && !inDisp {
				continue
			}
		}
		matched = append(matched, e)
	}

	total := len(matched)
	limit := p.Limit
	if p.AllRows {
		if limit <= 0 || limit > 10000 {
			limit = 10000
		}
	} else {
		if limit <= 0 || limit > 500 {
			limit = 100
		}
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}

	var paged []*models.LogEntry
	if offset < total {
		end := offset + limit
		if end > total {
			end = total
		}
		paged = matched[offset:end]
	} else {
		paged = []*models.LogEntry{}
	}

	return models.FilterResult{
		TotalFiltered: total,
		Limit:         limit,
		Offset:        offset,
		Entries:       paged,
	}
}

var (
	rePumpFast     = regexp.MustCompile(`(?i)(?:bomba|idContrato\s*=\s*'?)\s*(\d+)`)
	reDispatchFast = regexp.MustCompile(`CA_[a-f0-9-]{36}`)
	reDeviceFast   = regexp.MustCompile(`(?i)(?:Equipo=|dispositivo=)?(CCTALK\[\d+\]|LCDM1000\[\d+\]|MEI-SCx\[\d+\]|CCTALK|LCDM1000|MEI-SCx)`)
	reExFast       = regexp.MustCompile(`(System\.[A-Za-z0-9\.]+Exception)`)
)

func parseSingleLine(lineNum int, line string) *models.LogEntry {
	entry := &models.LogEntry{
		LineNumber: lineNum,
	}

	idx := 0
	if len(line) >= 24 && line[4] == '-' && line[7] == '-' && line[10] == ' ' && line[13] == ':' && line[16] == ':' && line[19] == '.' {
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

	for idx < len(line) && line[idx] == ' ' {
		idx++
	}

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

	for idx < len(line) && line[idx] == ' ' {
		idx++
	}

	rest := line[idx:]
	parts := strings.SplitN(rest, " ", 2)
	if len(parts) > 0 {
		entry.Component = parts[0]
		if len(parts) > 1 {
			msg := parts[1]
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

	if strings.Contains(fullText, "bomba") || strings.Contains(fullText, "Bomba") || strings.Contains(fullText, "idContrato") {
		if m := rePumpFast.FindStringSubmatch(fullText); len(m) > 1 {
			e.PumpID = m[1]
		}
	}

	if strings.Contains(fullText, "CA_") {
		if m := reDispatchFast.FindString(fullText); m != "" {
			e.DispatchID = m
		}
	}

	if strings.Contains(fullText, "CCTALK") || strings.Contains(fullText, "LCDM") || strings.Contains(fullText, "MEI") {
		if m := reDeviceFast.FindStringSubmatch(fullText); len(m) > 1 {
			e.Device = m[1]
		}
	}

	if strings.Contains(fullText, "Exception") {
		if m := reExFast.FindStringSubmatch(fullText); len(m) > 1 {
			e.ExceptionType = m[1]
		}
	}

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

// ParseSingleLine expone el parseador y clasificador de línea para paquetes externos
func ParseSingleLine(lineNum int, line string) *models.LogEntry {
	return parseSingleLine(lineNum, line)
}

// SeekToLine salta directamente a la posición de un checkpoint cercano a targetLine
func (l *IndexedLog) SeekToLine(file *os.File, targetLine int) (*bufio.Reader, int, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if targetLine <= 1 || len(l.Checkpoints) == 0 {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, 0, err
		}
		return bufio.NewReaderSize(file, 512*1024), 0, nil
	}

	offset := targetLine - 1
	cpIdx := offset / CheckpointInterval
	if cpIdx >= len(l.Checkpoints) {
		cpIdx = len(l.Checkpoints) - 1
	}

	if _, err := file.Seek(l.Checkpoints[cpIdx], io.SeekStart); err != nil {
		return nil, 0, err
	}
	return bufio.NewReaderSize(file, 512*1024), cpIdx * CheckpointInterval, nil
}

// GetLinesRange extrae un rango secuencial de líneas entre startLine y endLine (ambos inclusivos)
func (l *IndexedLog) GetLinesRange(startLine, endLine int) ([]*models.LogEntry, error) {
	if startLine < 1 {
		startLine = 1
	}
	if endLine < startLine {
		endLine = startLine
	}
	if endLine > l.TotalLines && l.TotalLines > 0 {
		endLine = l.TotalLines
	}

	file, err := os.Open(l.FilePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader, currentLine, err := l.SeekToLine(file, startLine)
	if err != nil {
		return nil, err
	}

	var entries []*models.LogEntry
	for {
		lineBytes, err := reader.ReadBytes('\n')
		if err != nil && len(lineBytes) == 0 {
			break
		}
		currentLine++
		if currentLine >= startLine && currentLine <= endLine {
			fullLine := bytes.TrimRight(lineBytes, "\r\n")
			rawLine := parser.Latin1ToUTF8(fullLine)
			entry := parseSingleLine(currentLine, rawLine)
			entries = append(entries, entry)
		}
		if currentLine >= endLine {
			break
		}
	}
	return entries, nil
}
