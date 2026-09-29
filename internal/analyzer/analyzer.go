package analyzer

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"interlog/internal/models"
)

var (
	reHWDevice = regexp.MustCompile(`(?:Equipo=|dispositivo=)([^,\s]+)`)
	reHWCode   = regexp.MustCompile(`Codigo=([^,\s]+)`)
	reHWDesc   = regexp.MustCompile(`Descripcion=([^,\n]+)`)
	reAmount   = regexp.MustCompile(`\|(\d+\.\d{2})`)
	reParamEx  = regexp.MustCompile(`par[aá]metro con nombre:?\s*([A-Za-z0-9_]+)`)
)

func Analyze(entries []*models.LogEntry, filePath string, parseDuration time.Duration, fileSizeMB float64) *models.Summary {
	s := &models.Summary{
		FilePath:        filePath,
		FileSizeMB:      fileSizeMB,
		ParseTimeMs:     parseDuration.Milliseconds(),
		TotalLines:      len(entries),
		LevelCounts:     make(map[string]int),
		CategoriesCount: make(map[string]int),
	}

	if len(entries) == 0 {
		return s
	}

	s.StartTime = entries[0].RawTime
	s.EndTime = entries[len(entries)-1].RawTime
	if !entries[0].Timestamp.IsZero() && !entries[len(entries)-1].Timestamp.IsZero() {
		dur := entries[len(entries)-1].Timestamp.Sub(entries[0].Timestamp)
		hours := int(dur.Hours())
		mins := int(dur.Minutes()) % 60
		secs := int(dur.Seconds()) % 60
		s.Duration = fmt.Sprintf("%02dh %02dm %02ds", hours, mins, secs)
	}

	// Temporary aggregators
	paymentsMap := make(map[string]*models.StuckPayment)
	hwAlertsMap := make(map[string]models.HardwareAlert)
	exMap := make(map[string]*models.ExceptionSummary)
	pumpMap := make(map[string]*models.PumpSummary)
	noiseMap := make(map[string]int)

	for _, e := range entries {
		s.LevelCounts[string(e.Level)]++
		s.CategoriesCount[string(e.Category)]++

		if e.IsNoise {
			s.NoiseCount++
			if strings.Contains(e.Component, "ObtenerParametro") {
				noiseMap["Parametros no encontrados (default)"]++
			} else if strings.Contains(e.Component, "Conectar") {
				noiseMap["Verificación conexión BD abierta"]++
			} else if strings.Contains(e.Component, "EvaluateQueryExecution") {
				noiseMap["Evaluación Resultsets simultáneos"]++
			} else if strings.Contains(e.Component, "transitions") || strings.Contains(e.Component, "Transition") {
				noiseMap["Transiciones pantalla de Bienvenida"]++
			} else {
				noiseMap["Otras rutinas de sondeo"]++
			}
		}

		if e.IsRealError {
			s.RealErrorsCount++
		}

		// 1. Stuck Payments tracking
		if e.DispatchID != "" {
			sp, exists := paymentsMap[e.DispatchID]
			if !exists {
				sp = &models.StuckPayment{
					DispatchID: e.DispatchID,
					FirstSeen:  e.RawTime,
					LastError:  "Pending registration",
				}
				paymentsMap[e.DispatchID] = sp
			}
			sp.LastSeen = e.RawTime
			if strings.Contains(e.Message, "TX: 10||") || strings.Contains(e.Component, "RegisterPaymentInformation") {
				sp.Retries++
			}
			if sp.Amount == "" {
				if m := reAmount.FindStringSubmatch(e.Message); len(m) > 1 {
					sp.Amount = "$" + m[1]
				}
			}
			if strings.Contains(e.Message, "ERROR|. Metodo RegistrarPago") {
				sp.LastError = "ERROR: Servidor rechazó RegistrarPago"
			}
		}

		// 2. Hardware device alerts
		if strings.Contains(e.Message, "Error en dispositivo=") || strings.Contains(e.Message, "ErrorDispositivo") {
			dev := ""
			if m := reHWDevice.FindStringSubmatch(e.Message); len(m) > 1 {
				dev = m[1]
			} else if e.Device != "" {
				dev = e.Device
			}
			code := ""
			if m := reHWCode.FindStringSubmatch(e.Message); len(m) > 1 {
				code = m[1]
			}
			desc := ""
			if m := reHWDesc.FindStringSubmatch(e.Message); len(m) > 1 {
				desc = m[1]
			} else {
				desc = e.Message
			}

			key := fmt.Sprintf("%s:%s:%s", dev, code, desc)
			if _, exists := hwAlertsMap[key]; !exists {
				hwAlertsMap[key] = models.HardwareAlert{
					Device:      dev,
					ErrorCode:   code,
					Description: desc,
					Timestamp:   e.RawTime,
					LineNumber:  e.LineNumber,
				}
			}
		}

		// 3. Exception breakdown
		if e.ExceptionType != "" {
			param := ""
			if m := reParamEx.FindStringSubmatch(e.Message); len(m) > 1 {
				param = m[1]
			}
			key := e.ExceptionType
			if param != "" {
				key = fmt.Sprintf("%s (%s)", e.ExceptionType, param)
			}
			exStat, exists := exMap[key]
			if !exists {
				exStat = &models.ExceptionSummary{
					Type:       e.ExceptionType,
					ParamName:  param,
					SampleLine: e.LineNumber,
					SampleMsg:  e.Message,
				}
				exMap[key] = exStat
			}
			exStat.Count++
		}

		// 4. Pump tracking
		if e.PumpID != "" {
			p, exists := pumpMap[e.PumpID]
			if !exists {
				p = &models.PumpSummary{
					PumpID: e.PumpID,
				}
				pumpMap[e.PumpID] = p
			}
			p.Mentions++
			if strings.Contains(e.Message, "Estado de la bomba") {
				parts := strings.Split(e.Message, ": ")
				if len(parts) > 1 {
					p.LastState = strings.TrimSpace(parts[1])
				}
			}
			if strings.Contains(e.Message, "Existen") && strings.Contains(e.Message, "operaciones abiertas") {
				p.OpenOpsFound++
			}
			if strings.Contains(e.Component, "CerrarOperacionFlow") {
				p.ClosedOpsCount++
			}
		}
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

	return s
}

func FilterEntries(entries []*models.LogEntry, p models.FilterParams) models.FilterResult {
	queryLower := strings.ToLower(strings.TrimSpace(p.Query))

	var matched []*models.LogEntry
	matched = make([]*models.LogEntry, 0, 1000)

	for _, e := range entries {
		if p.HideNoise && e.IsNoise {
			continue
		}
		if p.OnlyErrors && !e.IsRealError {
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
		if !p.FromTime.IsZero() && !e.Timestamp.IsZero() && e.Timestamp.Before(p.FromTime) {
			continue
		}
		if !p.ToTime.IsZero() && !e.Timestamp.IsZero() && e.Timestamp.After(p.ToTime) {
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
	if limit <= 0 || limit > 1000 {
		limit = 100
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
