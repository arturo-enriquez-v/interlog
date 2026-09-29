# ==============================================================================
# InterLog - Makefile
# ==============================================================================

BINARY_NAME ?= interlog
PORT ?= 8080
LOG_FILE ?= 20260515b.log.txt
GO ?= go

.DEFAULT_GOAL := help
.PHONY: help dev build run clean test summary errors payments hardware ai

## Ayuda / Lista de comandos
help:
	@echo "Uso: make [comando]"
	@echo ""
	@echo "Comandos disponibles:"
	@echo "  make dev        - Arranca el servidor web en modo desarrollo (http://localhost:$(PORT))"
	@echo "  make build      - Compila el binario de producción ($(BINARY_NAME))"
	@echo "  make run        - Compila (si es necesario) y ejecuta el servidor web"
	@echo "  make summary    - Ejecuta el análisis general del log por CLI"
	@echo "  make errors     - Muestra el desglose de errores reales por CLI"
	@echo "  make payments   - Muestra los pagos/despachos atorados por CLI"
	@echo "  make hardware   - Muestra el diagnóstico de hardware por CLI"
	@echo "  make ai         - Extrae contexto del error por CLI (ej: make ai ANCHOR=LCDM1000)"
	@echo "  make clean      - Elimina el binario compilado y archivos temporales"
	@echo "  make test       - Ejecuta las pruebas del proyecto"
	@echo ""
	@echo "Variables configurables:"
	@echo "  PORT=$(PORT)    - Puerto HTTP (ejemplo: make dev PORT=9000)"

## Modo desarrollo: arranca directamente con go run
dev:
	@echo "Iniciando InterLog en modo desarrollo en http://localhost:$(PORT)..."
	$(GO) run . serve --port $(PORT)

## Compilar el binario optimizado
build:
	@echo "Compilando $(BINARY_NAME)..."
	$(GO) build -ldflags="-s -w" -o $(BINARY_NAME) .
	@echo "Compilación exitosa: ./$(BINARY_NAME)"

## Ejecutar el binario compilado
run: build
	@echo "Iniciando ./$(BINARY_NAME) en http://localhost:$(PORT)..."
	./$(BINARY_NAME) serve --port $(PORT)

## Comandos CLI de análisis rápido
summary:
	$(GO) run . summary

errors:
	$(GO) run . errors

payments:
	$(GO) run . payments

hardware:
	$(GO) run . hardware

## Diagnóstico y extracción con IA
ai:
	$(GO) run . ai "$$(ANCHOR)"

## Tests
test:
	$(GO) test -v ./...

## Limpieza
clean:
	@echo "Limpiando binarios..."
	@rm -f $(BINARY_NAME)
	@echo "Limpieza completada."
