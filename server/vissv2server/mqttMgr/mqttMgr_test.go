/**
* Regression tests for the mqttMgr fixes shipped in ef639f0 (broker
* socket env var) and PR #121 (os.Exit removal).
**/
package mqttMgr

import (
	"strings"
	"testing"
)

// TestGetBrokerSocket_FallsBackToLocalhost is the regression test for
// the ef639f0 fix. Before that fix, MQTT_BROKER_ADDR was hardcoded to
// the public test.mosquitto.org broker — a production server that
// silently shipped traffic offsite if the env var wasn't set.
func TestGetBrokerSocket_FallsBackToLocalhost(t *testing.T) {
	t.Setenv("MQTT_BROKER_ADDR", "")
	got := getBrokerSocket(false)
	if !strings.Contains(got, "127.0.0.1") {
		t.Fatalf("expected fallback to 127.0.0.1 when MQTT_BROKER_ADDR is empty; got %q", got)
	}
	if strings.Contains(got, "mosquitto.org") {
		t.Fatalf("getBrokerSocket() leaked the old hardcoded mosquitto.org broker: %q", got)
	}
}

func TestGetBrokerSocket_HonoursEnvVar(t *testing.T) {
	t.Setenv("MQTT_BROKER_ADDR", "broker.example.invalid")
	got := getBrokerSocket(false)
	if !strings.Contains(got, "broker.example.invalid") {
		t.Fatalf("expected the env-supplied broker in result; got %q", got)
	}
}

func TestGetBrokerSocket_SecureUsesSSL(t *testing.T) {
	t.Setenv("MQTT_BROKER_ADDR", "broker.example.invalid")
	got := getBrokerSocket(true)
	if !strings.HasPrefix(got, "ssl://") {
		t.Fatalf("expected ssl:// prefix when secure=true; got %q", got)
	}
	if !strings.Contains(got, "8883") {
		t.Fatalf("expected secure MQTT port 8883; got %q", got)
	}
}

func TestGetBrokerSocket_InsecureUsesTCP(t *testing.T) {
	t.Setenv("MQTT_BROKER_ADDR", "broker.example.invalid")
	got := getBrokerSocket(false)
	if !strings.HasPrefix(got, "tcp://") {
		t.Fatalf("expected tcp:// prefix when secure=false; got %q", got)
	}
	if !strings.Contains(got, "1883") {
		t.Fatalf("expected MQTT port 1883; got %q", got)
	}
}
