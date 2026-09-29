# InterLog - Depurador y Analizador de Logs de Alta Velocidad (Go)

Herramienta desarrollada en **Go (sin dependencias externas)** para analizar, filtrar y depurar logs de gran volumen (archivos de más de 100 MB y 750,000+ líneas) en **menos de 1 segundo**.

Diseñado específicamente para la arquitectura de telemetría de cajeros y estaciones de servicio (**QuickPay / Interlogic**), con extracción automática de entidades críticas (bombas, dispensadores, billeteros, transacciones de pago y respuestas del socket gateway).

---

## ⚡ Rendimiento
- **Tiempo de carga y análisis:** ~850 ms para **747,308 líneas (109.6 MB)**.
- **Memoria:** Optimizado con lectura por streaming en bloques y buffers eficientes.
- **Soporte de Codificación:** Decodificación automática de ISO-8859-1 / Windows-1252 a UTF-8 (corrige acentos y caracteres especiales).
- **Cero dependencias externas:** Compila en un binario independiente de 10 MB con interfaz web embebida (`embed.FS`).

---

## 🔍 Hallazgos Principales en el Log `20260515b.log.txt`

1. **41,733 Errores Reales Ocultos**:
   - Aunque el log solo declara **8 líneas** con nivel formal `ERROR` y **1** con `WARN`, existen decenas de miles de fallas operativas logueadas erróneamente con nivel `INFO` o `DEBUG`.
2. **20 Pagos Atorados en Cola (Bucle Infinito de Reintentos)**:
   - Hay 20 despachos pendientes (`CA_...`, ej. `CA_4a66e4c0-bb26-4173-9fea-f804c73aa3a1`, con montos como $100.00, $50.00, $1000.00).
   - Cada pago ha sido reintentado más de **2,000 veces** porque el gateway responde `ERROR|. Metodo RegistrarPago`.
3. **Fallas Críticas en Dispositivos de Hardware del Cajero**:
   - `CCTALK[3]` (Monedero/Billetero): `ErrorGeneral` con código `30-00-00-3-00FF` (*Dispositivo no iniciado*).
   - `LCDM1000[5]` (Dispensador de billetes): `Error_al_abrir_el_puerto` (*No fue posible abrir el puerto serial/comunicación*).
4. **Excepciones de Código .NET**:
   - **1,028** ocurrencias de `System.InvalidCastException` en el parámetro `sensor_papel_habilitado`.
   - **516** ocurrencias de `System.InvalidCastException` en el parámetro `modo_quiosco`.
5. **145,019 Líneas de Ruido Operativo (19.4% del archivo)**:
   - Lecturas de parámetros inexistentes con valor por defecto, pings a conexiones ya abiertas de base de datos y temporizadores de transición de pantalla.

---

## 🚀 Uso Rápido (CLI)

### 1. Resumen Ejecutivo Completo
```bash
./interlog summary
# o con go:
go run . summary
```

### 2. Ver Desglose de Errores Reales
```bash
./interlog errors
```

### 3. Ver Pagos Atorados y Reintentos
```bash
./interlog payments
```

### 4. Diagnóstico de Dispositivos de Hardware
```bash
./interlog hardware
```

### 5. Rastrear el Ciclo de Vida de un Despacho o GUID
```bash
./interlog trace CA_0f71860d-58fa-4491-89bc-262f94017d6d
```

### 6. Búsqueda Instantánea con Filtro de Ruido
```bash
# Buscar eventos de un componente sin saturación de ruido:
./interlog search "LCDM1000" --no-noise

# Filtrar solo errores:
./interlog search "RegistrarPago" --errors

# Filtrar por nivel o categoría:
./interlog search "bomba" --cat PUMP --limit 20
```

---

## 🌐 Dashboard Web Interactivo Embebido

Para explorar con interfaz gráfica interactiva, modo oscuro y filtros en tiempo real:

```bash
# Con Make (Recomendado):
make dev

# O directamente con el binario / go:
./interlog serve --port 8080
# go run . serve --port 8080
```

Abre en tu navegador:
👉 **[http://localhost:8080](http://localhost:8080)**

### Características del Dashboard:
- **Gestión Multi-Archivo & Subida (Upload)**:
  - Selector de archivo activo en la barra superior (`20260515b.log.txt ▼`).
  - Botón **"+ Subir Log"** con zona *Drag & Drop* para cargar nuevos logs de hasta 500 MB.
  - Los archivos subidos se guardan en la carpeta `uploads/` y persisten entre reinicios.
  - Registro de logs con tamaño, fecha, número de líneas y errores reales para alternar entre archivos con un solo clic.
- **KPIs en tiempo real**: Líneas, duración, conteo real de errores, pagos atorados y porcentaje de ruido.
- **Explorador de Logs**: Búsqueda instantánea con debouncing, switch para **Ocultar Ruido (19%)** o **Solo Errores**, paginación y modal para ver consultas SQL multilínea y stacktraces.
- **Pestaña Pagos Atorados**: Tabla de los 20 despachos con montos, reintentos y botón directo de *Rastrear en Log*.
- **Pestaña Hardware**: Estado y códigos de falla de `CCTALK`, `LCDM1000` y `MEI-SCx`.
- **Pestaña Excepciones**: Agrupación por tipo y parámetro afectado.
- **Pestaña Análisis de Ruido**: Gráficos de qué rutinas saturan el archivo.

---

## 📁 Estructura del Proyecto
```
inter/
├── 20260515b.log.txt           # Archivo de log original (109.6 MB)
├── uploads/                    # Carpeta de almacenamiento para logs subidos
├── interlog                    # Binario compilado listo para ejecutar
├── main.go                     # Entrada CLI y enrutador de comandos
├── go.mod                      # Módulo Go estándar
├── internal/
│   ├── models/
│   │   └── models.go           # Estructuras de datos (LogEntry, Summary, FileMeta)
│   ├── parser/
│   │   └── parser.go           # Parser de streaming optimizado con Latin1->UTF8
│   ├── analyzer/
│   │   └── analyzer.go         # Agregaciones, métricas y motor de filtrado
│   ├── manager/
│   │   └── manager.go          # Registro de archivos, caché en RAM y uploads
│   └── web/
│       ├── server.go           # Servidor HTTP embebido y APIs REST
│       └── static/             # Assets embebidos (HTML5, CSS3, JS Vanilla)
│           ├── index.html
│           ├── style.css
└── README.md
```
