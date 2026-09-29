package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"interlog/internal/ai"
	"interlog/internal/analyzer"
	"interlog/internal/engine"
	"interlog/internal/manager"
	"interlog/internal/models"
	"interlog/internal/parser"
	"interlog/internal/web"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[37m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	command := os.Args[1]

	// Global file path flag default to the large log in root
	defaultLogPath := "20260515b.log.txt"
	if _, err := os.Stat(defaultLogPath); err != nil {
		// Try to find any .log or .log.txt file in current dir
		matches, _ := filepath.Glob("*.log*")
		if len(matches) > 0 {
			defaultLogPath = matches[0]
		}
	}

	switch command {
	case "summary", "stats":
		runSummary(defaultLogPath, os.Args[2:])
	case "errors":
		runErrors(defaultLogPath, os.Args[2:])
	case "payments":
		runPayments(defaultLogPath, os.Args[2:])
	case "hardware":
		runHardware(defaultLogPath, os.Args[2:])
	case "trace":
		runTrace(defaultLogPath, os.Args[2:])
	case "search":
		runSearch(defaultLogPath, os.Args[2:])
	case "serve", "ui", "web":
		runServe(defaultLogPath, os.Args[2:])
	case "ai", "gemini", "diagnose":
		runAI(defaultLogPath, os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		// If first argument is a file or search query
		if strings.HasSuffix(command, ".log") || strings.HasSuffix(command, ".txt") {
			runSummary(command, os.Args[2:])
		} else {
			fmt.Printf("%s[ERROR]%s Comando desconocido '%s'\n\n", colorRed, colorReset, command)
			printUsage()
		}
	}
}

func printUsage() {
	fmt.Printf("%s%s=== INTERLOG: Analizador de Logs de Alta Velocidad para QuickPay/Cajero ===%s\n\n", colorBold, colorCyan, colorReset)
	fmt.Println("Uso: interlog <comando> [opciones]")
	fmt.Println("\nComandos disponibles:")
	fmt.Printf("  %ssummary%s   Muestra métricas generales, errores reales, bombas, pagos atorados y ruido\n", colorBold, colorReset)
	fmt.Printf("  %serrors%s    Muestra todos los errores reales desglosados (hardware, excepciones, servidor)\n", colorBold, colorReset)
	fmt.Printf("  %spayments%s  Muestra el desglose de los pagos/despachos atorados en bucle de reintento\n", colorBold, colorReset)
	fmt.Printf("  %shardware%s  Muestra diagnósticos y fallas de dispositivos del cajero (CCTALK, LCDM, MEI)\n", colorBold, colorReset)
	fmt.Printf("  %strace%s     Rastrea un ID de despacho (CA_...) o GUID en toda su línea de tiempo\n", colorBold, colorReset)
	fmt.Printf("  %ssearch%s    Búsqueda rápida filtrable (con opción de ignorar ruido)\n", colorBold, colorReset)
	fmt.Printf("  %sserve%s     Inicia el Dashboard Web interactivo en http://localhost:8080\n", colorBold, colorReset)
	fmt.Printf("  %sai%s        Extrae contexto del error hacia abajo y opcionalmente analiza con Gemini\n", colorBold, colorReset)
	fmt.Println("\nEjemplos:")
	fmt.Println("  go run . summary")
	fmt.Println("  go run . errors")
	fmt.Println("  go run . payments")
	fmt.Println("  go run . hardware")
	fmt.Println("  go run . trace CA_4a66e4c0-bb26-4173-9fea-f804c73aa3a1")
	fmt.Println("  go run . search \"ObtenerEstadoBomba\" --no-noise")
	fmt.Println("  go run . search \"CCTALK\" --from \"2026-05-15 00:00:00\" --to \"2026-05-15 01:00:00\"")
	fmt.Println("  go run . serve --port 8080")
}

func loadLog(filePath string) ([]*models.LogEntry, *models.Summary, error) {
	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("no se pudo abrir el archivo de log '%s': %w", filePath, err)
	}

	sizeMB := float64(fi.Size()) / (1024 * 1024)
	fmt.Printf("%s[CARGANDO]%s Procesando '%s' (%.1f MB)...\n", colorDim, colorReset, filePath, sizeMB)

	entries, dur, err := parser.ParseFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("error al analizar archivo: %w", err)
	}

	summary := analyzer.Analyze(entries, filePath, dur, sizeMB)
	fmt.Printf("%s[LISTO]%s %s%d líneas%s procesadas en %s%d ms%s!\n\n",
		colorGreen, colorReset, colorBold, len(entries), colorReset, colorBold, dur.Milliseconds(), colorReset)

	return entries, summary, nil
}

func runSummary(filePath string, args []string) {
	_, s, err := loadLog(filePath)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	fmt.Printf("%s========================================================================%s\n", colorCyan, colorReset)
	fmt.Printf("%s                      RESUMEN EJECUTIVO DEL LOG                        %s\n", colorBold, colorReset)
	fmt.Printf("%s========================================================================%s\n", colorCyan, colorReset)
	fmt.Printf("Archivo:        %s%s%s (%.2f MB)\n", colorBold, s.FilePath, colorReset, s.FileSizeMB)
	fmt.Printf("Periodo:        %s%s%s  a  %s%s%s (Duración: %s%s%s)\n",
		colorBold, s.StartTime, colorReset, colorBold, s.EndTime, colorReset, colorBold, s.Duration, colorReset)
	fmt.Printf("Total líneas:   %s%d%s\n", colorBold, s.TotalLines, colorReset)

	fmt.Printf("\n%s[DISTRIBUCIÓN DE NIVELES DECLARADOS]%s\n", colorBold, colorReset)
	fmt.Printf("  DEBUG: %d | INFO: %d | WARN: %d | ERROR: %d\n",
		s.LevelCounts["DEBUG"], s.LevelCounts["INFO"], s.LevelCounts["WARN"], s.LevelCounts["ERROR"])

	fmt.Printf("\n%s[HALLAZGOS CRÍTICOS / ERRORES REALES]%s\n", colorBold, colorReset)
	fmt.Printf("  %s• Errores reales detectados:%s %s%d%s (¡Muchos enmascarados como INFO o DEBUG!)\n",
		colorYellow, colorReset, colorBold+colorRed, s.RealErrorsCount, colorReset)
	fmt.Printf("  %s• Pagos atorados en bucle:%s   %s%d transacciones%s reintentando indefinidamente\n",
		colorYellow, colorReset, colorBold+colorRed, len(s.StuckPayments), colorReset)
	fmt.Printf("  %s• Fallas de hardware cajero:%s %s%d eventos críticos%s\n",
		colorYellow, colorReset, colorBold+colorRed, len(s.HardwareAlerts), colorReset)
	fmt.Printf("  %s• Excepciones de sistema:%s    %s%d tipos distintos%s\n",
		colorYellow, colorReset, colorBold+colorRed, len(s.Exceptions), colorReset)
	fmt.Printf("  %s• Ruido operativo descartable:%s %s%d líneas (%.1f%% del log)%s\n",
		colorDim, colorReset, colorBold, s.NoiseCount, s.NoisePercent, colorReset)

	if len(s.Pumps) > 0 {
		fmt.Printf("\n%s[ESTADO DE BOMBAS]%s\n", colorBold, colorReset)
		for _, p := range s.Pumps {
			fmt.Printf("  - Bomba %s: %d menciones | Último estado: %s%s%s | Operaciones abiertas: %d\n",
				p.PumpID, p.Mentions, colorGreen, p.LastState, colorReset, p.OpenOpsFound)
		}
	}

	if len(s.HardwareAlerts) > 0 {
		fmt.Printf("\n%s[ALERTAS DE HARDWARE DETECTADAS]%s\n", colorBold+colorRed, colorReset)
		for _, h := range s.HardwareAlerts {
			fmt.Printf("  %s[%s]%s Dispositivo: %s%s%s | Código: %s | %s (Línea: %d)\n",
				colorYellow, h.Timestamp, colorReset, colorBold, h.Device, colorReset, h.ErrorCode, h.Description, h.LineNumber)
		}
	}

	if len(s.StuckPayments) > 0 {
		fmt.Printf("\n%s[TOP PAGOS ATORADOS (REINTENTOS EN BUCLE)]%s\n", colorBold+colorYellow, colorReset)
		max := 5
		if len(s.StuckPayments) < max {
			max = len(s.StuckPayments)
		}
		for i := 0; i < max; i++ {
			p := s.StuckPayments[i]
			fmt.Printf("  %d. Despacho: %s%s%s | Monto: %s%s%s | Reintentos: %s%d%s | %s\n",
				i+1, colorBold, p.DispatchID, colorReset, colorGreen, p.Amount, colorReset, colorRed, p.Retries, colorReset, p.LastError)
		}
		if len(s.StuckPayments) > 5 {
			fmt.Printf("  %s... y %d pagos más atorados. Ejecuta 'go run . payments' para ver la lista completa.%s\n",
				colorDim, len(s.StuckPayments)-5, colorReset)
		}
	}

	if len(s.Exceptions) > 0 {
		fmt.Printf("\n%s[TOP EXCEPCIONES ENCONTRADAS]%s\n", colorBold+colorRed, colorReset)
		for _, ex := range s.Exceptions {
			param := ""
			if ex.ParamName != "" {
				param = fmt.Sprintf(" (Parámetro: %s)", ex.ParamName)
			}
			fmt.Printf("  - %s%s%s: %s%d veces%s%s (Ej: Línea %d)\n",
				colorBold, ex.Type, colorReset, colorRed, ex.Count, colorReset, param, ex.SampleLine)
		}
	}

	if len(s.NoiseBreakdown) > 0 {
		fmt.Printf("\n%s[PATRONES DE RUIDO PRINCIPALES]%s\n", colorDim, colorReset)
		for _, nb := range s.NoiseBreakdown {
			fmt.Printf("  - %-40s %6d líneas (%.1f%%)\n", nb.Pattern, nb.Count, nb.Percent)
		}
	}

	fmt.Printf("\n%sTip:%s Puedes iniciar la interfaz web interactiva con: %sgo run . serve%s\n\n",
		colorCyan, colorReset, colorBold+colorGreen, colorReset)
}

func runErrors(filePath string, args []string) {
	_, s, err := loadLog(filePath)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	fmt.Printf("%s========================================================================%s\n", colorRed, colorReset)
	fmt.Printf("%s                     DESGLOSE DE ERRORES REALES                        %s\n", colorBold, colorReset)
	fmt.Printf("%s========================================================================%s\n\n", colorRed, colorReset)

	fmt.Printf("%s1. HARDWARE Y DISPOSITIVOS DEL CAJERO (%d eventos)%s\n", colorBold+colorYellow, len(s.HardwareAlerts), colorReset)
	for _, h := range s.HardwareAlerts {
		fmt.Printf("  • [%s] %s%s%s - %s\n", h.Timestamp, colorBold, h.Device, colorReset, h.Description)
		fmt.Printf("    Código: %s | Línea: %d\n\n", h.ErrorCode, h.LineNumber)
	}

	fmt.Printf("%s2. EXCEPCIONES .NET (%d tipos de excepciones)%s\n", colorBold+colorRed, len(s.Exceptions), colorReset)
	for _, ex := range s.Exceptions {
		param := ""
		if ex.ParamName != "" {
			param = fmt.Sprintf(" [Parámetro '%s']", ex.ParamName)
		}
		fmt.Printf("  • %s%s%s%s: %d ocurrencias\n", colorBold, ex.Type, colorReset, param, ex.Count)
		fmt.Printf("    Muestra (Línea %d): %s%s%s\n\n", ex.SampleLine, colorDim, ex.SampleMsg, colorReset)
	}

	fmt.Printf("%s3. RECHAZOS DEL GATEWAY / SERVIDOR DE PAGOS (%d despachos afectados)%s\n", colorBold+colorPurple, len(s.StuckPayments), colorReset)
	for _, p := range s.StuckPayments {
		fmt.Printf("  • Despacho: %s%s%s | Monto: %s | Reintentos: %s%d%s | %s\n",
			colorBold, p.DispatchID, colorReset, p.Amount, colorRed, p.Retries, colorReset, p.LastError)
	}
}

func runPayments(filePath string, args []string) {
	_, s, err := loadLog(filePath)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	fmt.Printf("%s========================================================================%s\n", colorYellow, colorReset)
	fmt.Printf("%s             PAGOS ATORADOS EN COLA DE REINTENTOS (%d)                 %s\n", colorBold, len(s.StuckPayments), colorReset)
	fmt.Printf("%s========================================================================%s\n\n", colorYellow, colorReset)

	fmt.Printf("%-38s | %-8s | %-9s | %-23s | %s\n", "ID DE DESPACHO", "MONTO", "REINTENTOS", "ÚLTIMO REINTENTO", "RESPUESTA")
	fmt.Println(strings.Repeat("-", 105))
	for _, p := range s.StuckPayments {
		fmt.Printf("%-38s | %-8s | %-9d | %-23s | %s%s%s\n",
			p.DispatchID, p.Amount, p.Retries, p.LastSeen, colorRed, p.LastError, colorReset)
	}

	fmt.Printf("\n%sCausa raíz identificada:%s El gateway local devuelve 'ERROR|. Metodo RegistrarPago'\n", colorBold+colorYellow, colorReset)
	fmt.Println("El sistema reintenta periódicamente enviar estos 20 despachos en cada ciclo de corte/pago.")
	fmt.Println("Para ver el flujo de un pago en particular, ejecuta: go run . trace <despacho_id>")
}

func runHardware(filePath string, args []string) {
	_, s, err := loadLog(filePath)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	fmt.Printf("%s========================================================================%s\n", colorCyan, colorReset)
	fmt.Printf("%s             DIAGNÓSTICO DE DISPOSITIVOS DE HARDWARE                   %s\n", colorBold, colorReset)
	fmt.Printf("%s========================================================================%s\n\n", colorCyan, colorReset)

	for _, h := range s.HardwareAlerts {
		fmt.Printf("%s[%s]%s Dispositivo: %s%s%s\n", colorDim, h.Timestamp, colorReset, colorBold+colorRed, h.Device, colorReset)
		fmt.Printf("  Código:      %s\n", h.ErrorCode)
		fmt.Printf("  Descripción: %s\n", h.Description)
		fmt.Printf("  Línea:       %d\n\n", h.LineNumber)
	}
}

func runTrace(filePath string, args []string) {
	if len(args) == 0 {
		fmt.Printf("%sError:%s Debe especificar un ID para rastrear. Ej: go run . trace CA_4a66e4c0-bb26-4173-9fea-f804c73aa3a1\n", colorRed, colorReset)
		return
	}

	target := args[0]
	entries, _, err := loadLog(filePath)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	fmt.Printf("%sRastreando eventos para '%s%s%s':%s\n\n", colorBold, colorCyan, target, colorReset, colorBold)

	count := 0
	for _, e := range entries {
		if strings.Contains(e.DispatchID, target) || strings.Contains(e.GUID, target) || strings.Contains(e.Message, target) {
			count++
			if count > 50 {
				fmt.Printf("... y más eventos coincidentes. (Mostrando primeros 50)\n")
				break
			}
			levelColor := colorGreen
			if e.Level == models.LevelError || e.IsRealError {
				levelColor = colorRed
			} else if e.Level == models.LevelWarn {
				levelColor = colorYellow
			}
			fmt.Printf("%s[%s]%s %s%-5s%s %s%s%s %s\n",
				colorDim, e.RawTime, colorReset, levelColor, e.Level, colorReset, colorBold, e.Component, colorReset, e.Message)
		}
	}

	if count == 0 {
		fmt.Printf("No se encontraron eventos para '%s'.\n", target)
	} else {
		fmt.Printf("\nTotal de eventos encontrados: %d\n", count)
	}
}

func runSearch(filePath string, args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	hideNoise := fs.Bool("no-noise", false, "Ocultar ruido repetitivo")
	onlyErrors := fs.Bool("errors", false, "Solo mostrar errores reales")
	limit := fs.Int("limit", 50, "Cantidad máxima de resultados")
	level := fs.String("level", "", "Filtrar por nivel (DEBUG, INFO, WARN, ERROR)")
	category := fs.String("cat", "", "Filtrar por categoría (PAYMENT, HARDWARE, PUMP, DATABASE, SOCKET, EXCEPTION)")
	pumpID := fs.String("pump", "", "Filtrar por ID de bomba (ej: 3)")
	fromStr := fs.String("from", "", "Hora/fecha inicial (ej: '2026-05-15 00:00:00' o '2026-05-15T00:00')")
	toStr := fs.String("to", "", "Hora/fecha final (ej: '2026-05-15 01:00:00' o '2026-05-15T01:00')")

	if len(args) == 0 {
		fmt.Println("Uso: interlog search <texto> [flags]")
		fs.PrintDefaults()
		return
	}

	query := args[0]
	fs.Parse(args[1:])

	entries, _, err := loadLog(filePath)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	fromTime, _ := engine.ParseFlexibleTime(*fromStr)
	toTime, _ := engine.ParseFlexibleTime(*toStr)

	params := models.FilterParams{
		Query:      query,
		HideNoise:  *hideNoise,
		OnlyErrors: *onlyErrors,
		Limit:      *limit,
		Level:      models.LogLevel(*level),
		Category:   models.Category(*category),
		PumpID:     *pumpID,
		FromTime:   fromTime,
		ToTime:     toTime,
		RawFrom:    *fromStr,
		RawTo:      *toStr,
	}

	res := analyzer.FilterEntries(entries, params)

	fmt.Printf("%sBúsqueda para '%s'%s (Coincidencias totales: %s%d%s, Mostrando: %d):\n\n",
		colorBold, query, colorReset, colorCyan, res.TotalFiltered, colorReset, len(res.Entries))

	for _, e := range res.Entries {
		levelColor := colorGreen
		if e.Level == models.LevelError || e.IsRealError {
			levelColor = colorRed
		} else if e.Level == models.LevelWarn {
			levelColor = colorYellow
		} else if e.Level == models.LevelDebug {
			levelColor = colorDim
		}
		fmt.Printf("%s[%s]%s %s%-5s%s %sL%-6d%s %s%s%s: %s\n",
			colorDim, e.RawTime, colorReset,
			levelColor, e.Level, colorReset,
			colorDim, e.LineNumber, colorReset,
			colorBold, e.Component, colorReset,
			e.Message)
	}
}

func runServe(filePath string, args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 8080, "Puerto para el servidor web")
	fs.Parse(args)

	mgr := manager.NewLogManager(".", "uploads")
	fmt.Printf("%s[INICIANDO]%s Registrando archivos de log disponibles...\n", colorDim, colorReset)
	if err := mgr.InitActive(filePath); err != nil {
		fmt.Printf("%sAdvertencia al cargar archivo inicial:%s %v\n", colorYellow, colorReset, err)
	}

	server := web.NewServer(mgr, *port)
	if err := server.Start(); err != nil {
		fmt.Printf("%sError al iniciar servidor:%s %v\n", colorRed, colorReset, err)
	}
}

func runAI(filePath string, args []string) {
	fs := flag.NewFlagSet("ai", flag.ExitOnError)
	anchor := fs.String("anchor", "", "Término ancla o componente para iniciar la búsqueda")
	startLine := fs.Int("line", 0, "Número de línea inicial")
	before := fs.Int("before", 15, "Líneas de contexto previas al error")
	after := fs.Int("after", 15, "Líneas de contexto posteriores al error")
	maxScan := fs.Int("scan", 500, "Máximo de líneas hacia abajo para buscar el error")
	noNoise := fs.Bool("no-noise", true, "Ocultar ruido operativo repetitivo")
	analyze := fs.Bool("analyze", false, "Enviar a Google Gemini API para obtener diagnóstico")
	apiKey := fs.String("key", "", "Google AI Studio API Key (o definir GEMINI_API_KEY)")
	model := fs.String("model", "gemini-2.5-flash", "Modelo de Gemini a utilizar")
	prompt := fs.String("prompt", "", "Instrucción o pregunta adicional para la IA")

	// Normalizar argumentos para permitir banderas después de argumentos posicionales
	var flagsOnly, posArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flagsOnly = append(flagsOnly, arg)
			// Si la bandera toma un valor separado por espacio (ej. --before 15)
			if !strings.Contains(arg, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				// Revisar si es una bandera booleana (sin valor)
				flagName := strings.TrimLeft(arg, "-")
				if flagName != "analyze" && flagName != "no-noise" {
					i++
					flagsOnly = append(flagsOnly, args[i])
				}
			}
		} else {
			posArgs = append(posArgs, arg)
		}
	}
	fs.Parse(append(flagsOnly, posArgs...))

	if *anchor == "" && *startLine <= 0 && fs.NArg() > 0 {
		*anchor = fs.Arg(0)
	}

	if *anchor == "" && *startLine <= 0 {
		fmt.Printf("%s[ERROR]%s Debes indicar un parámetro ancla (--anchor o argumento) o número de línea (--line)\n", colorRed, colorReset)
		fmt.Println("Ejemplo: interlog ai \"LCDM1000\" --before 15 --after 15")
		fmt.Println("         interlog ai \"RegistrarPago\" --analyze --key \"AIzaSy...\"")
		return
	}

	fmt.Printf("%s[INICIANDO]%s Procesando archivo '%s'...\n", colorDim, colorReset, filePath)
	idxLog, err := engine.IndexFile(filePath)
	if err != nil {
		fmt.Printf("%s[ERROR]%s No se pudo indexar archivo: %v\n", colorRed, colorReset, err)
		return
	}

	req := ai.ContextExtractRequest{
		Anchor:      *anchor,
		StartLine:   *startLine,
		LinesBefore: *before,
		LinesAfter:  *after,
		MaxScan:     *maxScan,
		HideNoise:   *noNoise,
	}

	res, err := ai.ExtractContextDownwards(idxLog, req)
	if err != nil {
		fmt.Printf("%s[ERROR]%s Error al extraer contexto: %v\n", colorRed, colorReset, err)
		return
	}

	if !res.Success && !res.AnchorFound {
		fmt.Printf("%s[AVISO]%s %s\n", colorYellow, colorReset, res.Message)
		return
	}

	fmt.Println()
	fmt.Printf("%s=== EXTRACTO DE TELEMETRÍA PREPARADO PARA IA ===%s\n", colorBold, colorReset)
	fmt.Printf("🎯 Ancla: %s%s%s (Línea #%d)\n", colorCyan, res.Anchor, colorReset, res.AnchorLine)
	if res.ErrorFound {
		fmt.Printf("🚨 Error detectado: %sLínea #%d%s (a %d líneas del ancla)\n", colorRed, res.ErrorLine, colorReset, res.ErrorDistance)
		fmt.Printf("🏷️ Motivo: %s%s%s\n", colorYellow, res.ErrorReason, colorReset)
	} else {
		fmt.Printf("⚠️ %s\n", res.Message)
	}
	fmt.Printf("🪙 Tokens estimados: %s~%d tokens%s\n\n", colorGreen, res.EstimatedTokens, colorReset)

	fmt.Println(res.ContextText)
	fmt.Println()

	if *analyze {
		fmt.Printf("%s[GEMINI]%s Consultando a Google Gemini (%s)...\n", colorPurple, colorReset, *model)
		diagReq := ai.DiagnosisRequest{
			ContextText:  res.ContextText,
			CustomPrompt: *prompt,
			ApiKey:       *apiKey,
			Model:        *model,
		}
		diagRes, err := ai.CallGemini(diagReq)
		if err != nil {
			fmt.Printf("%s[ERROR GEMINI]%s %v\n", colorRed, colorReset, err)
			return
		}

		fmt.Printf("\n%s=== DIAGNÓSTICO DE IA (Gemini - %dms) ===%s\n\n", colorBold, diagRes.LatencyMs, colorReset)
		fmt.Println(diagRes.Analysis)
	} else {
		fmt.Printf("%s[TIP]%s Para mandar a analizar con Gemini desde la CLI, agrega %s--analyze%s (y opcionalmente %s--key TU_API_KEY%s)\n",
			colorDim, colorReset, colorBold, colorReset, colorBold, colorReset)
	}
}
