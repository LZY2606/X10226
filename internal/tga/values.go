package tga

import (
	"fmt"
	"strconv"
	"strings"
)

func quantityFromMap(mp map[string]interface{}, key string) Quantity {
	value, ok := mp[key]
	if !ok {
		return Quantity{Diagnosis: "字段缺失"}
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		raw, _ := typed["raw"].(string)
		unit, _ := typed["unit"].(string)
		if number, ok := numberFromInterface(typed["value"]); ok {
			return Quantity{Raw: raw, Value: number, Unit: unit, Valid: true}
		}
		if raw == "" {
			raw = fmt.Sprint(typed)
		}
		return Quantity{Raw: raw, Unit: unit, Diagnosis: "缺少可转换数值"}
	case float64:
		return Quantity{Raw: strconv.FormatFloat(typed, 'f', -1, 64), Value: typed, Valid: true}
	case int:
		return Quantity{Raw: strconv.Itoa(typed), Value: float64(typed), Valid: true}
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return Quantity{Raw: typed, Diagnosis: "数值无法解析"}
		}
		return Quantity{Raw: typed, Value: number, Valid: true}
	default:
		return Quantity{Raw: fmt.Sprint(value), Diagnosis: "不支持的数值表示"}
	}
}

func normalizeTime(q Quantity) Quantity {
	if !q.Valid {
		return q
	}
	unit := strings.ToLower(strings.TrimSpace(q.Unit))
	switch unit {
	case "", "min", "minute", "minutes", "分钟":
		q.Unit = "min"
	case "s", "sec", "second", "seconds", "秒":
		q.Value /= 60
		q.Unit = "min"
	default:
		q.Valid = false
		q.Diagnosis = "无法把时间单位转换为 min"
	}
	return q
}

func normalizeTemperature(q Quantity) Quantity {
	if !q.Valid {
		return q
	}
	unit := strings.ToLower(strings.TrimSpace(q.Unit))
	switch unit {
	case "", "c", "°c", "degc", "摄氏度":
		q.Unit = "°C"
	case "k":
		q.Value -= 273.15
		q.Unit = "°C"
	default:
		q.Valid = false
		q.Diagnosis = "无法把温度单位转换为 °C"
	}
	return q
}

func normalizeMass(q Quantity) Quantity {
	if !q.Valid {
		return q
	}
	unit := strings.ToLower(strings.TrimSpace(q.Unit))
	switch unit {
	case "", "mg", "毫克":
		q.Unit = "mg"
	case "g":
		q.Value *= 1000
		q.Unit = "mg"
	case "ug", "µg":
		q.Value /= 1000
		q.Unit = "mg"
	default:
		q.Valid = false
		q.Diagnosis = "无法把质量单位转换为 mg"
	}
	return q
}

func payloadArray(mp map[string]interface{}, key string) []interface{} {
	value, ok := mp[key].([]interface{})
	if !ok {
		return nil
	}
	return value
}

func payloadObject(value interface{}) map[string]interface{} {
	mp, _ := value.(map[string]interface{})
	if mp == nil {
		return map[string]interface{}{}
	}
	return mp
}

func payloadBool(mp map[string]interface{}, key string, fallback bool) bool {
	value, ok := mp[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true"
	default:
		return fallback
	}
}

func numberFromInterface(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	default:
		return 0, false
	}
}

func judgment(code, severity, message string, objects ...string) Judgment {
	return Judgment{Code: code, Severity: severity, Message: message, ObjectKeys: objects, RuleVersion: AlgorithmCandidates}
}

func formatFloat(value float64, digits int) string {
	return strconv.FormatFloat(value, 'f', digits, 64)
}
