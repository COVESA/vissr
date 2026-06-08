package utils

import (
	"strings"
	"testing"
)

// helpers

func actuatorNode(name, datatype, min, max string, allowed []string) *Node_t {
	n := NewSignalNode(name, ACTUATOR, datatype, "test signal", min, max, "")
	n.AllowedDef = allowed
	if len(allowed) > 0 {
		n.Allowed = uint8(len(allowed))
	}
	return n
}

func sensorNode(name, datatype string) *Node_t {
	return NewSignalNode(name, SENSOR, datatype, "test signal", "", "", "")
}

func attributeNode(name, datatype string) *Node_t {
	return NewSignalNode(name, ATTRIBUTE, datatype, "test attr", "", "", "")
}

// ── nil / unknown type ────────────────────────────────────────────────────────

func TestValidateSetValue_NilNode(t *testing.T) {
	err := ValidateSetValue(nil, "42")
	if err == nil {
		t.Fatal("expected error for nil node")
	}
	ve := err.(*ValidationError)
	if ve.Code != 400 {
		t.Errorf("code = %d, want 400", ve.Code)
	}
}

func TestValidateSetValue_SensorRejected(t *testing.T) {
	err := ValidateSetValue(sensorNode("Speed", "float"), "42")
	if err == nil {
		t.Fatal("expected error for sensor SET")
	}
	ve := err.(*ValidationError)
	if ve.Code != 403 {
		t.Errorf("code = %d, want 403", ve.Code)
	}
	if !strings.Contains(ve.Message, "read-only") {
		t.Errorf("message = %q, want 'read-only'", ve.Message)
	}
}

func TestValidateSetValue_BranchRejected(t *testing.T) {
	n := NewBranchNode("Vehicle")
	err := ValidateSetValue(n, "value")
	if err == nil {
		t.Fatal("expected error for branch SET")
	}
}

func TestValidateSetValue_ActuatorAccepted(t *testing.T) {
	n := actuatorNode("IsEnabled", "boolean", "", "", nil)
	if err := ValidateSetValue(n, "true"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateSetValue_AttributeAccepted(t *testing.T) {
	n := attributeNode("VIN", "string")
	if err := ValidateSetValue(n, "ABC123"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateSetValue_NoDatatype(t *testing.T) {
	n := &Node_t{Name: "X", NodeType: ACTUATOR}
	if err := ValidateSetValue(n, "anything"); err != nil {
		t.Errorf("no datatype should accept any value: %v", err)
	}
}

// ── allowed values ────────────────────────────────────────────────────────────

func TestValidateSetValue_AllowedAccepted(t *testing.T) {
	n := actuatorNode("Gear", "string", "", "", []string{"Park", "Reverse", "Neutral", "Drive"})
	for _, v := range []string{"Park", "Reverse", "Neutral", "Drive"} {
		if err := ValidateSetValue(n, v); err != nil {
			t.Errorf("value %q should be allowed: %v", v, err)
		}
	}
}

func TestValidateSetValue_AllowedCaseInsensitive(t *testing.T) {
	n := actuatorNode("Gear", "string", "", "", []string{"Park", "Drive"})
	if err := ValidateSetValue(n, "park"); err != nil {
		t.Errorf("case-insensitive match should pass: %v", err)
	}
}

func TestValidateSetValue_AllowedRejected(t *testing.T) {
	n := actuatorNode("Gear", "string", "", "", []string{"Park", "Drive"})
	err := ValidateSetValue(n, "Sport")
	if err == nil {
		t.Fatal("expected error for value not in allowed list")
	}
	if !strings.Contains(err.Error(), "not in allowed set") {
		t.Errorf("message = %q, want 'not in allowed set'", err.Error())
	}
}

// ── boolean ───────────────────────────────────────────────────────────────────

func TestValidateSetValue_BooleanValid(t *testing.T) {
	n := actuatorNode("ABS", "boolean", "", "", nil)
	for _, v := range []string{"true", "false", "1", "0", "True", "FALSE"} {
		if err := ValidateSetValue(n, v); err != nil {
			t.Errorf("value %q should be valid boolean: %v", v, err)
		}
	}
}

func TestValidateSetValue_BooleanInvalid(t *testing.T) {
	n := actuatorNode("ABS", "boolean", "", "", nil)
	for _, v := range []string{"yes", "no", "2", "enabled", ""} {
		if err := ValidateSetValue(n, v); err == nil {
			t.Errorf("value %q should fail boolean check", v)
		}
	}
}

// ── int types ─────────────────────────────────────────────────────────────────

func TestValidateSetValue_Int8Valid(t *testing.T) {
	n := actuatorNode("Temp", "int8", "", "", nil)
	for _, v := range []string{"-128", "0", "127"} {
		if err := ValidateSetValue(n, v); err != nil {
			t.Errorf("value %q should be valid int8: %v", v, err)
		}
	}
}

func TestValidateSetValue_Int8Overflow(t *testing.T) {
	n := actuatorNode("Temp", "int8", "", "", nil)
	for _, v := range []string{"128", "-129", "999"} {
		if err := ValidateSetValue(n, v); err == nil {
			t.Errorf("value %q should overflow int8", v)
		}
	}
}

func TestValidateSetValue_Uint8Valid(t *testing.T) {
	n := actuatorNode("Pos", "uint8", "", "", nil)
	for _, v := range []string{"0", "128", "255"} {
		if err := ValidateSetValue(n, v); err != nil {
			t.Errorf("value %q: %v", v, err)
		}
	}
}

func TestValidateSetValue_Uint8Overflow(t *testing.T) {
	n := actuatorNode("Pos", "uint8", "", "", nil)
	if err := ValidateSetValue(n, "256"); err == nil {
		t.Error("256 should overflow uint8")
	}
	if err := ValidateSetValue(n, "-1"); err == nil {
		t.Error("-1 should fail uint8")
	}
}

func TestValidateSetValue_Int16Range(t *testing.T) {
	n := actuatorNode("X", "int16", "", "", nil)
	if err := ValidateSetValue(n, "32767"); err != nil {
		t.Errorf("max int16 failed: %v", err)
	}
	if err := ValidateSetValue(n, "32768"); err == nil {
		t.Error("32768 should overflow int16")
	}
}

func TestValidateSetValue_Uint16Range(t *testing.T) {
	n := actuatorNode("X", "uint16", "", "", nil)
	if err := ValidateSetValue(n, "65535"); err != nil {
		t.Errorf("max uint16 failed: %v", err)
	}
	if err := ValidateSetValue(n, "65536"); err == nil {
		t.Error("65536 should overflow uint16")
	}
}

func TestValidateSetValue_Int32Range(t *testing.T) {
	n := actuatorNode("X", "int32", "", "", nil)
	if err := ValidateSetValue(n, "2147483647"); err != nil {
		t.Errorf("max int32 failed: %v", err)
	}
	if err := ValidateSetValue(n, "2147483648"); err == nil {
		t.Error("2147483648 should overflow int32")
	}
}

func TestValidateSetValue_Int64Valid(t *testing.T) {
	n := actuatorNode("X", "int64", "", "", nil)
	if err := ValidateSetValue(n, "9223372036854775807"); err != nil {
		t.Errorf("max int64 failed: %v", err)
	}
	if err := ValidateSetValue(n, "not-a-number"); err == nil {
		t.Error("expected error for non-numeric int64")
	}
}

func TestValidateSetValue_Uint64Valid(t *testing.T) {
	n := actuatorNode("X", "uint64", "", "", nil)
	if err := ValidateSetValue(n, "18446744073709551615"); err != nil {
		t.Errorf("max uint64 failed: %v", err)
	}
	if err := ValidateSetValue(n, "-1"); err == nil {
		t.Error("expected error for negative uint64")
	}
}

// ── float / double ────────────────────────────────────────────────────────────

func TestValidateSetValue_FloatValid(t *testing.T) {
	n := actuatorNode("Speed", "float", "", "", nil)
	for _, v := range []string{"0", "3.14", "-42.5", "1e3"} {
		if err := ValidateSetValue(n, v); err != nil {
			t.Errorf("value %q: %v", v, err)
		}
	}
}

func TestValidateSetValue_FloatNaN(t *testing.T) {
	n := actuatorNode("Speed", "float", "", "", nil)
	if err := ValidateSetValue(n, "NaN"); err == nil {
		t.Error("NaN should be rejected for float")
	}
}

func TestValidateSetValue_FloatInf(t *testing.T) {
	n := actuatorNode("Speed", "float", "", "", nil)
	for _, v := range []string{"+Inf", "-Inf", "Inf"} {
		if err := ValidateSetValue(n, v); err == nil {
			t.Errorf("Inf value %q should be rejected for float", v)
		}
	}
}

func TestValidateSetValue_DoubleValid(t *testing.T) {
	n := actuatorNode("Lat", "double", "", "", nil)
	if err := ValidateSetValue(n, "-90.0"); err != nil {
		t.Errorf("valid double failed: %v", err)
	}
}

// ── range checking ────────────────────────────────────────────────────────────

func TestValidateSetValue_FloatBelowMin(t *testing.T) {
	n := actuatorNode("Speed", "float", "0", "250", nil)
	if err := ValidateSetValue(n, "-1"); err == nil {
		t.Error("expected range error below min")
	}
}

func TestValidateSetValue_FloatAboveMax(t *testing.T) {
	n := actuatorNode("Speed", "float", "0", "250", nil)
	if err := ValidateSetValue(n, "251"); err == nil {
		t.Error("expected range error above max")
	}
}

func TestValidateSetValue_FloatAtBoundary(t *testing.T) {
	n := actuatorNode("Speed", "float", "0", "250", nil)
	for _, v := range []string{"0", "250", "125.5"} {
		if err := ValidateSetValue(n, v); err != nil {
			t.Errorf("boundary value %q should pass: %v", v, err)
		}
	}
}

func TestValidateSetValue_IntBelowMin(t *testing.T) {
	n := actuatorNode("Gear", "uint8", "1", "5", nil)
	if err := ValidateSetValue(n, "0"); err == nil {
		t.Error("expected range error below uint8 min=1")
	}
}

func TestValidateSetValue_IntAboveMax(t *testing.T) {
	n := actuatorNode("Gear", "uint8", "1", "5", nil)
	if err := ValidateSetValue(n, "6"); err == nil {
		t.Error("expected range error above uint8 max=5")
	}
}

func TestValidateSetValue_SignedIntRange(t *testing.T) {
	n := actuatorNode("Temp", "int16", "-40", "85", nil)
	if err := ValidateSetValue(n, "-40"); err != nil {
		t.Errorf("boundary -40 should pass: %v", err)
	}
	if err := ValidateSetValue(n, "-41"); err == nil {
		t.Error("expected error below int16 min=-40")
	}
}

func TestValidateSetValue_StringNoRange(t *testing.T) {
	n := actuatorNode("Name", "string", "0", "100", nil)
	if err := ValidateSetValue(n, "hello"); err != nil {
		t.Errorf("string type should ignore range: %v", err)
	}
}

func TestValidateSetValue_UnknownDatatype(t *testing.T) {
	n := actuatorNode("Custom", "vehiclegearposition", "", "", nil)
	if err := ValidateSetValue(n, "Drive"); err != nil {
		t.Errorf("unknown datatype should accept any value: %v", err)
	}
}

// ── ValidationError ───────────────────────────────────────────────────────────

func TestValidationError_Message(t *testing.T) {
	e := &ValidationError{Code: 400, Message: "test error"}
	if e.Error() != "test error" {
		t.Errorf("Error() = %q, want 'test error'", e.Error())
	}
}

// ── containsAllowed ───────────────────────────────────────────────────────────

func TestContainsAllowed_True(t *testing.T) {
	if !containsAllowed([]string{"A", "B", "C"}, "B") {
		t.Error("B should be found")
	}
}

func TestContainsAllowed_CaseInsensitive(t *testing.T) {
	if !containsAllowed([]string{"Park"}, "PARK") {
		t.Error("case-insensitive match should return true")
	}
}

func TestContainsAllowed_False(t *testing.T) {
	if containsAllowed([]string{"A", "B"}, "C") {
		t.Error("C should not be found")
	}
}

func TestContainsAllowed_EmptyList(t *testing.T) {
	if containsAllowed([]string{}, "A") {
		t.Error("empty list should return false")
	}
}

// ── Fuzz ──────────────────────────────────────────────────────────────────────

func FuzzValidateSetValue(f *testing.F) {
	// Seed: (datatype, value, min, max)
	f.Add("float", "42.5", "0", "100")
	f.Add("uint8", "255", "0", "255")
	f.Add("int16", "-32768", "-32768", "32767")
	f.Add("boolean", "true", "", "")
	f.Add("string", "hello world", "", "")
	f.Add("", "anything", "", "")
	f.Add("double", "NaN", "", "")

	f.Fuzz(func(t *testing.T, datatype, value, minStr, maxStr string) {
		n := actuatorNode("X", datatype, minStr, maxStr, nil)
		// Must not panic on any input combination.
		_ = ValidateSetValue(n, value)
	})
}
