package models

import "time"

type LogLevel string

const (
	LevelDebug LogLevel = "DEBUG"
	LevelInfo  LogLevel = "INFO"
	LevelWarn  LogLevel = "WARN"
	LevelError LogLevel = "ERROR"
)

type Category string

const (
	CategoryPayment   Category = "PAYMENT"
	CategoryHardware  Category = "HARDWARE"
	CategoryPump      Category = "PUMP"
	CategoryDatabase  Category = "DATABASE"
	CategorySocket    Category = "SOCKET"
	CategoryException Category = "EXCEPTION"
	CategoryRoutine   Category = "ROUTINE"
)

type LogEntry struct {
	LineNumber    int       `json:"line_number"`
	Timestamp     time.Time `json:"timestamp"`
	RawTime       string    `json:"raw_time"`
	Level         LogLevel  `json:"level"`
	Component     string    `json:"component"`
	GUID          string    `json:"guid,omitempty"`
	Message       string    `json:"message"`
	Category      Category  `json:"category"`
	IsNoise       bool      `json:"is_noise"`
	IsRealError   bool      `json:"is_real_error"`
	ErrorReason   string    `json:"error_reason,omitempty"`
	PumpID        string    `json:"pump_id,omitempty"`
	DispatchID    string    `json:"dispatch_id,omitempty"`
	Device        string    `json:"device,omitempty"`
	ExceptionType string    `json:"exception_type,omitempty"`
}

type StuckPayment struct {
	DispatchID string `json:"dispatch_id"`
	Amount     string `json:"amount"`
	Retries    int    `json:"retries"`
	FirstSeen  string `json:"first_seen"`
	LastSeen   string `json:"last_seen"`
	LastError  string `json:"last_error"`
}

type HardwareAlert struct {
	Device      string `json:"device"`
	ErrorCode   string `json:"error_code"`
	Description string `json:"description"`
	Timestamp   string `json:"timestamp"`
	LineNumber  int    `json:"line_number"`
}

type FileMeta struct {
	Name       string  `json:"name"`
	Path       string  `json:"path"`
	SizeMB     float64 `json:"size_mb"`
	ModTime    string  `json:"mod_time"`
	IsActive   bool    `json:"is_active"`
	IsUploaded bool    `json:"is_uploaded"`
	TotalLines int     `json:"total_lines,omitempty"`
	ErrorCount int     `json:"error_count,omitempty"`
	Duration   string  `json:"duration,omitempty"`
	Parsed     bool    `json:"parsed"`
}

type ExceptionSummary struct {
	Type       string `json:"type"`
	Count      int    `json:"count"`
	ParamName  string `json:"param_name,omitempty"`
	SampleMsg  string `json:"sample_msg"`
	SampleLine int    `json:"sample_line"`
}

type PumpSummary struct {
	PumpID         string `json:"pump_id"`
	Mentions       int    `json:"mentions"`
	LastState      string `json:"last_state"`
	OpenOpsFound   int    `json:"open_ops_found"`
	ClosedOpsCount int    `json:"closed_ops_count"`
}

type NoiseStat struct {
	Pattern string  `json:"pattern"`
	Count   int     `json:"count"`
	Percent float64 `json:"percent"`
}

type Summary struct {
	FilePath        string             `json:"file_path"`
	FileSizeMB      float64            `json:"file_size_mb"`
	ParseTimeMs     int64              `json:"parse_time_ms"`
	TotalLines      int                `json:"total_lines"`
	StartTime       string             `json:"start_time"`
	EndTime         string             `json:"end_time"`
	Duration        string             `json:"duration"`
	LevelCounts     map[string]int     `json:"level_counts"`
	RealErrorsCount int                `json:"real_errors_count"`
	NoiseCount      int                `json:"noise_count"`
	NoisePercent    float64            `json:"noise_percent"`
	CategoriesCount map[string]int     `json:"categories_count"`
	StuckPayments   []StuckPayment     `json:"stuck_payments"`
	HardwareAlerts  []HardwareAlert    `json:"hardware_alerts"`
	Exceptions      []ExceptionSummary `json:"exceptions"`
	Pumps           []PumpSummary      `json:"pumps"`
	NoiseBreakdown  []NoiseStat        `json:"noise_breakdown"`
}

type FilterParams struct {
	Query      string    `json:"query"`
	Level      LogLevel  `json:"level"`
	Category   Category  `json:"category"`
	PumpID     string    `json:"pump_id"`
	DispatchID string    `json:"dispatch_id"`
	Device     string    `json:"device"`
	FromTime   time.Time `json:"from_time"`
	ToTime     time.Time `json:"to_time"`
	RawFrom    string    `json:"raw_from"`
	RawTo      string    `json:"raw_to"`
	HideNoise  bool      `json:"hide_noise"`
	OnlyErrors bool      `json:"only_errors"`
	AllRows    bool      `json:"all_rows"`
	AroundLine int       `json:"around_line"`
	Radius     int       `json:"radius"`
	Limit      int       `json:"limit"`
	Offset     int       `json:"offset"`
}

type FilterResult struct {
	TotalFiltered int         `json:"total_filtered"`
	Limit         int         `json:"limit"`
	Offset        int         `json:"offset"`
	Entries       []*LogEntry `json:"entries"`
}
