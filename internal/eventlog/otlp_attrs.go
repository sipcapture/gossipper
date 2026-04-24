package eventlog

import (
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

type attributeKV struct {
	Key   string
	Value any
}

func kvsToAttrs(kvs []attributeKV) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(kvs))
	for _, kv := range kvs {
		out = append(out, anyToAttribute(kv.Key, kv.Value))
	}
	return out
}

func anyToAttribute(k string, v any) attribute.KeyValue {
	switch val := v.(type) {
	case nil:
		return attribute.String(k, "")
	case string:
		return attribute.String(k, val)
	case bool:
		return attribute.Bool(k, val)
	case int:
		return attribute.Int64(k, int64(val))
	case int32:
		return attribute.Int64(k, int64(val))
	case int64:
		return attribute.Int64(k, val)
	case uint:
		return attribute.Int64(k, int64(val))
	case uint32:
		return attribute.Int64(k, int64(val))
	case uint64:
		return attribute.Int64(k, int64(val))
	case float32:
		return attribute.Float64(k, float64(val))
	case float64:
		return attribute.Float64(k, val)
	case time.Duration:
		return attribute.String(k, val.String())
	case time.Time:
		return attribute.String(k, val.Format(time.RFC3339Nano))
	case error:
		return attribute.String(k, val.Error())
	default:
		return attribute.String(k, fmt.Sprintf("%v", val))
	}
}
