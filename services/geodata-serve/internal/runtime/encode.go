package runtime

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/duckdb/duckdb-go/v2"
)

func encodeValue(value any, typeName string) (any, error) {
	if value == nil {
		return nil, nil
	}
	upperType := strings.ToUpper(typeName)
	if strings.HasSuffix(upperType, "[]") || strings.HasPrefix(upperType, "STRUCT(") || strings.HasPrefix(upperType, "MAP(") || strings.HasPrefix(upperType, "UNION(") {
		return encodeNested(value)
	}
	if isDecimalType(upperType) || isBigIntegerType(upperType) {
		return decimalString(value)
	}
	if upperType == "JSON" {
		return encodeJSON(value)
	}
	if strings.Contains(upperType, "FLOAT") || strings.Contains(upperType, "DOUBLE") {
		return encodeFloat(value)
	}
	if strings.Contains(upperType, "BLOB") || strings.Contains(upperType, "GEOMETRY") {
		return encodedBlob(value)
	}
	if upperType == "UUID" {
		return encodedUUID(value)
	}
	if isTemporalType(upperType) {
		if interval, ok := value.(duckdb.Interval); ok {
			return intervalString(interval), nil
		}
		if timestamp, ok := value.(time.Time); ok {
			return temporalString(timestamp, upperType), nil
		}
		return fmt.Sprint(value), nil
	}
	return encodeNested(value)
}

func encodeNested(value any) (any, error) {
	switch value := value.(type) {
	case nil:
		return nil, nil
	case bool, string, int, int8, int16, int32, uint, uint8, uint16, uint32:
		return value, nil
	case int64, uint64:
		return fmt.Sprint(value), nil
	case float32, float64:
		return encodeFloat(value)
	case json.Number:
		if !validJSONNumber(value.String()) {
			return nil, fmt.Errorf("invalid JSON number %q", value)
		}
		return value, nil
	case *big.Int:
		if value == nil {
			return nil, nil
		}
		return value.String(), nil
	case duckdb.Decimal:
		return value.String(), nil
	case time.Time:
		return temporalString(value, "TIMESTAMP"), nil
	case duckdb.Interval:
		return intervalString(value), nil
	case []byte:
		return encodedBlob(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			var err error
			out[i], err = encodeNested(item)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			var err error
			out[key], err = encodeNested(item)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	case duckdb.Map:
		return encodeMap(value)
	case duckdb.Union:
		encoded, err := encodeNested(value.Value)
		if err != nil {
			return nil, err
		}
		return map[string]any{"tag": value.Tag, "value": encoded}, nil
	default:
		if stringer, ok := value.(fmt.Stringer); ok {
			return stringer.String(), nil
		}
		return nil, fmt.Errorf("unsupported result value %T", value)
	}
}

func encodeJSON(value any) (any, error) {
	var source []byte
	switch value := value.(type) {
	case string:
		source = []byte(value)
	case []byte:
		source = value
	default:
		return encodeDecodedJSON(value)
	}
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode JSON value: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return nil, errors.New("JSON value contains multiple documents")
		}
		return nil, fmt.Errorf("finish JSON value: %w", err)
	}
	return encodeNested(decoded)
}

func encodeDecodedJSON(value any) (any, error) {
	switch value := value.(type) {
	case float64:
		if math.Trunc(value) == value && math.Abs(value) >= 1<<53 {
			return nil, fmt.Errorf("JSON integer %v may have lost precision in the DuckDB driver", value)
		}
		return encodeFloat(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			var err error
			out[i], err = encodeDecodedJSON(item)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			var err error
			out[key], err = encodeDecodedJSON(item)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	default:
		return encodeNested(value)
	}
}

func validJSONNumber(value string) bool {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return false
	}
	_, ok := decoded.(json.Number)
	if !ok {
		return false
	}
	return decoder.Decode(new(any)) == io.EOF
}

func decimalString(value any) (any, error) {
	switch value := value.(type) {
	case *big.Int:
		if value == nil {
			return nil, nil
		}
		return value.String(), nil
	case big.Int:
		return value.String(), nil
	case duckdb.Decimal:
		return value.String(), nil
	case int:
		return fmt.Sprintf("%d", value), nil
	case int8:
		return fmt.Sprintf("%d", value), nil
	case int16:
		return fmt.Sprintf("%d", value), nil
	case int32:
		return fmt.Sprintf("%d", value), nil
	case int64:
		return fmt.Sprintf("%d", value), nil
	case uint:
		return fmt.Sprintf("%d", value), nil
	case uint8:
		return fmt.Sprintf("%d", value), nil
	case uint16:
		return fmt.Sprintf("%d", value), nil
	case uint32:
		return fmt.Sprintf("%d", value), nil
	case uint64:
		return fmt.Sprintf("%d", value), nil
	default:
		return nil, fmt.Errorf("cannot encode %s value %T as decimal string", typeNameForError(value), value)
	}
}

func encodeFloat(value any) (any, error) {
	var number float64
	switch value := value.(type) {
	case float32:
		number = float64(value)
	case float64:
		number = value
	default:
		return nil, fmt.Errorf("cannot encode floating point value %T", value)
	}
	if math.IsNaN(number) {
		return "NaN", nil
	}
	if math.IsInf(number, 1) {
		return "Infinity", nil
	}
	if math.IsInf(number, -1) {
		return "-Infinity", nil
	}
	if _, ok := value.(float32); ok {
		return float32(number), nil
	}
	return number, nil
}

func encodedBlob(value any) (any, error) {
	data, ok := value.([]byte)
	if !ok {
		return nil, fmt.Errorf("cannot encode binary value %T", value)
	}
	return map[string]any{"encoding": "base64", "data": base64.StdEncoding.EncodeToString(data)}, nil
}

func encodedUUID(value any) (any, error) {
	data, ok := value.([]byte)
	if !ok || len(data) != 16 {
		return nil, fmt.Errorf("cannot encode UUID value %T", value)
	}
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", data[:4], data[4:6], data[6:8], data[8:10], data[10:]), nil
}

func isBigIntegerType(name string) bool {
	return name == "BIGINT" || name == "UBIGINT" || name == "HUGEINT" || name == "UHUGEINT" || name == "BIGNUM"
}

func isDecimalType(name string) bool { return strings.HasPrefix(name, "DECIMAL(") || name == "DECIMAL" }

func isTemporalType(name string) bool {
	return strings.HasPrefix(name, "DATE") || strings.HasPrefix(name, "TIME") || strings.HasPrefix(name, "TIMESTAMP") || name == "INTERVAL"
}

func temporalString(value time.Time, typeName string) string {
	switch {
	case strings.HasPrefix(typeName, "DATE"):
		return value.Format("2006-01-02")
	case strings.HasPrefix(typeName, "TIME"):
		return value.Format("15:04:05.999999999Z07:00")
	default:
		return value.Format(time.RFC3339Nano)
	}
}

func intervalString(value duckdb.Interval) string {
	return fmt.Sprintf("%d months %d days %d micros", value.Months, value.Days, value.Micros)
}

func encodeMap(value duckdb.Map) (any, error) {
	allStringKeys := true
	for key := range value {
		if _, ok := key.(string); !ok {
			allStringKeys = false
			break
		}
	}
	if allStringKeys {
		out := make(map[string]any, len(value))
		for key, item := range value {
			encoded, err := encodeNested(item)
			if err != nil {
				return nil, err
			}
			out[key.(string)] = encoded
		}
		return out, nil
	}

	type mapEntry struct {
		key   any
		text  string
		value any
	}
	entries := make([]mapEntry, 0, len(value))
	for key, item := range value {
		encodedKey, err := encodeNested(key)
		if err != nil {
			return nil, err
		}
		encodedValue, err := encodeNested(item)
		if err != nil {
			return nil, err
		}
		entries = append(entries, mapEntry{key: encodedKey, text: fmt.Sprint(encodedKey), value: encodedValue})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].text < entries[j].text })
	out := make([]any, len(entries))
	for i, entry := range entries {
		out[i] = map[string]any{"key": entry.key, "value": entry.value}
	}
	return out, nil
}

func typeNameForError(value any) string {
	if value == nil {
		return "NULL"
	}
	return reflect.TypeOf(value).String()
}
