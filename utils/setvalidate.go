package utils

// setvalidate.go validates a VISS SET value against the declared metadata
// (datatype, min, max, allowed) of a Node_t signal.
//
// Usage:
//
//	if err := ValidateSetValue(node, value); err != nil {
//	    // return VISS error 400 with err.Error() as the message
//	}

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ValidationError is returned when a SET value fails validation.
// Code is the VISS error code (400 = bad request, 403 = forbidden).
type ValidationError struct {
	Code    int
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// ValidateSetValue checks that value is acceptable for the given node.
// It returns nil if the value is valid, or a *ValidationError otherwise.
//
// Checks performed (in order):
//  1. Node type — only actuator and attribute nodes can be SET.
//  2. Allowed values — if AllowedDef is non-empty, value must be in the list.
//  3. Datatype — value must parse as the declared datatype.
//  4. Range — for numeric types, min/max are enforced when declared.
func ValidateSetValue(node *Node_t, value string) error {
	if node == nil {
		return &ValidationError{400, "signal not found"}
	}

	// Only actuators and attributes can be written.
	switch node.NodeType {
	case ACTUATOR, ATTRIBUTE:
		// writable
	case SENSOR:
		return &ValidationError{403, fmt.Sprintf("signal %q is read-only (sensor)", node.Name)}
	default:
		return &ValidationError{400, fmt.Sprintf("node %q is not a signal (type=%s)", node.Name, node.NodeType)}
	}

	// Allowed-value check takes priority: if the list is defined, the value
	// must appear in it regardless of datatype/range.
	if len(node.AllowedDef) > 0 {
		if !containsAllowed(node.AllowedDef, value) {
			return &ValidationError{400, fmt.Sprintf(
				"value %q not in allowed set [%s]",
				value, strings.Join(node.AllowedDef, ", "),
			)}
		}
		return nil // allowed check is definitive; skip further checks
	}

	dt := strings.ToLower(strings.TrimSpace(node.Datatype))
	if dt == "" {
		return nil // no declared datatype — accept anything
	}

	if err := checkDatatype(dt, value); err != nil {
		return &ValidationError{400, err.Error()}
	}

	// Range check (numeric types only).
	if node.Min != "" || node.Max != "" {
		if err := checkRange(dt, value, node.Min, node.Max, node.Name); err != nil {
			return &ValidationError{400, err.Error()}
		}
	}

	return nil
}

// ── Datatype checking ─────────────────────────────────────────────────────────

func checkDatatype(dt, value string) error {
	switch dt {
	case "boolean":
		v := strings.ToLower(value)
		if v != "true" && v != "false" && v != "1" && v != "0" {
			return fmt.Errorf("value %q is not a valid boolean (expected true/false/1/0)", value)
		}

	case "int8":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("value %q is not a valid int8: %v", value, err)
		}
		if n < math.MinInt8 || n > math.MaxInt8 {
			return fmt.Errorf("value %q out of int8 range [%d, %d]", value, math.MinInt8, math.MaxInt8)
		}
	case "int16":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("value %q is not a valid int16: %v", value, err)
		}
		if n < math.MinInt16 || n > math.MaxInt16 {
			return fmt.Errorf("value %q out of int16 range [%d, %d]", value, math.MinInt16, math.MaxInt16)
		}
	case "int32":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("value %q is not a valid int32: %v", value, err)
		}
		if n < math.MinInt32 || n > math.MaxInt32 {
			return fmt.Errorf("value %q out of int32 range [%d, %d]", value, math.MinInt32, math.MaxInt32)
		}
	case "int64":
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return fmt.Errorf("value %q is not a valid int64: %v", value, err)
		}

	case "uint8":
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("value %q is not a valid uint8: %v", value, err)
		}
		if n > math.MaxUint8 {
			return fmt.Errorf("value %q out of uint8 range [0, %d]", value, math.MaxUint8)
		}
	case "uint16":
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("value %q is not a valid uint16: %v", value, err)
		}
		if n > math.MaxUint16 {
			return fmt.Errorf("value %q out of uint16 range [0, %d]", value, math.MaxUint16)
		}
	case "uint32":
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("value %q is not a valid uint32: %v", value, err)
		}
		if n > math.MaxUint32 {
			return fmt.Errorf("value %q out of uint32 range [0, %d]", value, math.MaxUint32)
		}
	case "uint64":
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			return fmt.Errorf("value %q is not a valid uint64: %v", value, err)
		}

	case "float":
		f, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return fmt.Errorf("value %q is not a valid float: %v", value, err)
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("value %q is NaN or Inf, not allowed for float", value)
		}
	case "double":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("value %q is not a valid double: %v", value, err)
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("value %q is NaN or Inf, not allowed for double", value)
		}

	case "string":
		// Any UTF-8 string is valid.

	default:
		// Unknown or composite datatype (e.g. "string[]") — accept the value.
	}
	return nil
}

// checkRange validates that value (already parsed to the correct type) falls
// within [min, max].  Both bounds are optional (empty string = unbounded).
func checkRange(dt, value, minStr, maxStr, signalName string) error {
	isFloat := dt == "float" || dt == "double"
	isUnsigned := strings.HasPrefix(dt, "uint")
	isInteger := strings.HasPrefix(dt, "int") || isUnsigned

	if !isFloat && !isInteger {
		return nil // range only applies to numeric types
	}

	if isFloat {
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil // datatype check already reported this
		}
		if minStr != "" {
			minV, err := strconv.ParseFloat(minStr, 64)
			if err == nil && v < minV {
				return fmt.Errorf("%s: value %s below minimum %s", signalName, value, minStr)
			}
		}
		if maxStr != "" {
			maxV, err := strconv.ParseFloat(maxStr, 64)
			if err == nil && v > maxV {
				return fmt.Errorf("%s: value %s above maximum %s", signalName, value, maxStr)
			}
		}
		return nil
	}

	// Integer range.
	if isUnsigned {
		v, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil
		}
		if minStr != "" {
			minV, err := strconv.ParseUint(minStr, 10, 64)
			if err == nil && v < minV {
				return fmt.Errorf("%s: value %s below minimum %s", signalName, value, minStr)
			}
		}
		if maxStr != "" {
			maxV, err := strconv.ParseUint(maxStr, 10, 64)
			if err == nil && v > maxV {
				return fmt.Errorf("%s: value %s above maximum %s", signalName, value, maxStr)
			}
		}
		return nil
	}

	v, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil
	}
	if minStr != "" {
		minV, err := strconv.ParseInt(minStr, 10, 64)
		if err == nil && v < minV {
			return fmt.Errorf("%s: value %s below minimum %s", signalName, value, minStr)
		}
	}
	if maxStr != "" {
		maxV, err := strconv.ParseInt(maxStr, 10, 64)
		if err == nil && v > maxV {
			return fmt.Errorf("%s: value %s above maximum %s", signalName, value, maxStr)
		}
	}
	return nil
}

// containsAllowed reports whether value is in the allowed list
// (case-insensitive comparison).
func containsAllowed(allowed []string, value string) bool {
	v := strings.ToLower(value)
	for _, a := range allowed {
		if strings.ToLower(a) == v {
			return true
		}
	}
	return false
}
