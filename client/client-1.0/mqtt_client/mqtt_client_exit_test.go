/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests that the MQTT broker functions of mqtt_client report a broker that cannot be reached to their
* caller, instead of ending the process. The test runs the function in a child process (the test binary
* itself), and looks at how the child ended.
**/
package main

import (
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// childEnv is set in the child process: it tells the test binary which function to run.
const childEnv = "MQTT_CLIENT_TEST_CHILD"

// unusedAddress returns a local TCP address that nothing listens on.
func unusedAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// TestMain lets the test binary act as the child process.
func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "":
		os.Exit(m.Run())
	case "subscribe":
		mqttSubscribe("tcp://"+os.Getenv("BROKER"), "VISS/reply")
		os.Stdout.WriteString("CHILD: mqttSubscribe returned\n")
	case "publish":
		publishMessage("tcp://"+os.Getenv("BROKER"), "/V/Vehicle", "{}")
		os.Stdout.WriteString("CHILD: publishMessage returned\n")
	}
	os.Exit(0)
}

func runChild(t *testing.T, which string) (output string, exitCode int) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), childEnv+"="+which, "BROKER="+unusedAddress(t))
	out, err := cmd.CombinedOutput()
	output = string(out)
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return output, ee.ExitCode()
		}
		t.Fatalf("child did not run: %v", err)
	}
	return output, 0
}

// With no broker the function must return, so that the caller can decide what to do.
func TestMqttSubscribe_UnreachableBrokerReturnsToTheCaller(t *testing.T) {
	out, code := runChild(t, "subscribe")
	if code != 0 || !strings.Contains(out, "CHILD: mqttSubscribe returned") {
		t.Errorf("the process ended with exit code %d inside mqttSubscribe instead of returning\n%s", code, out)
	}
}

func TestPublishMessage_UnreachableBrokerReturnsToTheCaller(t *testing.T) {
	out, code := runChild(t, "publish")
	if code != 0 || !strings.Contains(out, "CHILD: publishMessage returned") {
		t.Errorf("the process ended with exit code %d inside publishMessage instead of returning\n%s", code, out)
	}
}
