package journald

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

var standardPassthroughFields = map[string]struct{}{
	"CODE_FILE":     {},
	"CODE_FUNC":     {},
	"CODE_LINE":     {},
	"DOCUMENTATION": {},
	"ERRNO":         {},
	"MESSAGE_ID":    {},
	"TID":           {},
}

// Options configures the native journald slog handler.
type Options struct {
	Identifier string
	Level      slog.Leveler
	AddSource  bool
	Sender     Sender
}

// Handler writes slog records to the native journald socket.
type Handler struct {
	identifier string
	level      slog.Leveler
	addSource  bool
	sender     Sender
	attrs      []slog.Attr
	groups     []string
}

// NewHandler builds a journald-backed slog.Handler.
func NewHandler(opts Options) (*Handler, error) {
	if strings.TrimSpace(opts.Identifier) == "" {
		return nil, errors.New("journald handler requires an identifier")
	}

	level := opts.Level
	if level == nil {
		level = slog.LevelInfo
	}

	sender := opts.Sender
	if sender == nil {
		var err error
		sender, err = NewSender(DefaultSocketPath)
		if err != nil {
			return nil, err
		}
	}

	return &Handler{
		identifier: opts.Identifier,
		level:      level,
		addSource:  opts.AddSource,
		sender:     sender,
	}, nil
}

func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *Handler) Handle(_ context.Context, record slog.Record) error {
	fields := map[string]string{
		"MESSAGE":           record.Message,
		"PRIORITY":          priorityForLevel(record.Level),
		"SYSLOG_IDENTIFIER": h.identifier,
	}

	if h.addSource && record.PC != 0 {
		addSourceFields(fields, record.PC)
	}

	for _, attr := range h.attrs {
		addAttr(fields, h.groups, attr)
	}

	record.Attrs(func(attr slog.Attr) bool {
		addAttr(fields, h.groups, attr)
		return true
	})

	payload := make([]Field, 0, len(fields))
	for name, value := range fields {
		payload = append(payload, Field{Name: name, Value: value})
	}
	sort.Slice(payload, func(i, j int) bool { return payload[i].Name < payload[j].Name })
	return h.sender.Send(payload)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(slices.Clone(h.attrs), attrs...)
	return &clone
}

func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.groups = append(slices.Clone(h.groups), name)
	return &clone
}

func addSourceFields(fields map[string]string, pc uintptr) {
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	if frame.File != "" {
		if _, exists := fields["CODE_FILE"]; !exists {
			fields["CODE_FILE"] = frame.File
		}
	}
	if frame.Function != "" {
		if _, exists := fields["CODE_FUNC"]; !exists {
			fields["CODE_FUNC"] = frame.Function
		}
	}
	if frame.Line != 0 {
		if _, exists := fields["CODE_LINE"]; !exists {
			fields["CODE_LINE"] = strconv.Itoa(frame.Line)
		}
	}
}

func addAttr(fields map[string]string, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}

	if attr.Value.Kind() == slog.KindGroup {
		nextGroups := groups
		if attr.Key != "" {
			nextGroups = append(slices.Clone(groups), attr.Key)
		}
		for _, child := range attr.Value.Group() {
			addAttr(fields, nextGroups, child)
		}
		return
	}

	fieldName := fieldNameForAttr(groups, attr.Key)
	if fieldName == "" {
		return
	}

	fieldValue, ok := fieldValueForAttr(attr.Value)
	if !ok {
		return
	}
	fields[fieldName] = fieldValue
}

func fieldNameForAttr(groups []string, key string) string {
	parts := make([]string, 0, len(groups)+1)
	for _, group := range groups {
		if normalized := normalizeFieldComponent(group); normalized != "" {
			parts = append(parts, normalized)
		}
	}
	if normalized := normalizeFieldComponent(key); normalized != "" {
		parts = append(parts, normalized)
	}
	if len(parts) == 0 {
		return ""
	}

	name := strings.Join(parts, "_")
	if _, ok := standardPassthroughFields[name]; ok {
		return name
	}
	if strings.HasPrefix(name, "LINUXIO_") {
		return name
	}
	return "LINUXIO_" + name
}

func normalizeFieldComponent(component string) string {
	component = strings.TrimSpace(component)
	if component == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(component))
	lastUnderscore := false
	for _, r := range component {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r - ('a' - 'A'))
			lastUnderscore = false
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(builder.String(), "_")
}

func fieldValueForAttr(value slog.Value) (string, bool) {
	switch value.Kind() {
	case slog.KindBool:
		return strconv.FormatBool(value.Bool()), true
	case slog.KindDuration:
		return value.Duration().String(), true
	case slog.KindFloat64:
		return strconv.FormatFloat(value.Float64(), 'g', -1, 64), true
	case slog.KindInt64:
		return strconv.FormatInt(value.Int64(), 10), true
	case slog.KindString:
		return value.String(), true
	case slog.KindTime:
		return value.Time().Format(time.RFC3339Nano), true
	case slog.KindUint64:
		return strconv.FormatUint(value.Uint64(), 10), true
	case slog.KindAny:
		return encodeAnyValue(value.Any())
	default:
		return "", false
	}
}

func encodeAnyValue(v any) (string, bool) {
	switch value := v.(type) {
	case nil:
		return "", false
	case error:
		return value.Error(), true
	case time.Time:
		return value.Format(time.RFC3339Nano), true
	case time.Duration:
		return value.String(), true
	case fmt.Stringer:
		return value.String(), true
	case string:
		return value, true
	case bool:
		return strconv.FormatBool(value), true
	case int:
		return strconv.Itoa(value), true
	case int8:
		return strconv.FormatInt(int64(value), 10), true
	case int16:
		return strconv.FormatInt(int64(value), 10), true
	case int32:
		return strconv.FormatInt(int64(value), 10), true
	case int64:
		return strconv.FormatInt(value, 10), true
	case uint:
		return strconv.FormatUint(uint64(value), 10), true
	case uint8:
		return strconv.FormatUint(uint64(value), 10), true
	case uint16:
		return strconv.FormatUint(uint64(value), 10), true
	case uint32:
		return strconv.FormatUint(uint64(value), 10), true
	case uint64:
		return strconv.FormatUint(value, 10), true
	case float32:
		return strconv.FormatFloat(float64(value), 'g', -1, 32), true
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64), true
	}

	buf, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v), true
	}
	return string(buf), true
}

func priorityForLevel(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return "7"
	case level < slog.LevelWarn:
		return "6"
	case level < slog.LevelError:
		return "4"
	default:
		return "3"
	}
}
