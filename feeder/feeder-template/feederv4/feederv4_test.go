/**
* Regression-adjacent tests for the feederv4 unsubscribe fix in PR #121.
*
* The fix itself lives inline inside a goroutine case statement and is
* not separately callable. The correctness of the fix depends on
* onNotificationList returning the *correct* index into notificationList
* (so slices.Delete(notificationList, idx, idx+1) acts on the right
* slot). The tests here pin that helper's contract; the inline call
* site is exercised by integration tests (runtest.sh + testClient).
**/
package main

import (
	"testing"
)

// TestOnNotificationList_FindsExistingPath verifies the lookup returns
// the in-list index, which the PR #121 fix now feeds into slices.Delete
// — replacing the buggy use of the unsubscribe-message loop counter.
func TestOnNotificationList_FindsExistingPath(t *testing.T) {
	saved := notificationList
	defer func() { notificationList = saved }()
	notificationList = []string{"Vehicle.Speed", "Vehicle.Engine.Rpm", "Vehicle.Cabin.Temperature"}

	cases := map[string]int{
		"Vehicle.Speed":             0,
		"Vehicle.Engine.Rpm":        1,
		"Vehicle.Cabin.Temperature": 2,
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			if got := onNotificationList(path); got != want {
				t.Fatalf("onNotificationList(%q) = %d; want %d", path, got, want)
			}
		})
	}
}

// TestOnNotificationList_ReturnsMinusOneForUnknown verifies the lookup
// returns -1 for absent paths. The PR #121 fix guards on this return
// value before calling slices.Delete.
func TestOnNotificationList_ReturnsMinusOneForUnknown(t *testing.T) {
	saved := notificationList
	defer func() { notificationList = saved }()
	notificationList = []string{"Vehicle.Speed"}

	cases := []string{
		"Vehicle.NotKnown",
		"",
		"Vehicle.Speed.Suffix",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			if got := onNotificationList(p); got != -1 {
				t.Fatalf("onNotificationList(%q) = %d; want -1", p, got)
			}
		})
	}
}

// TestOnNotificationList_EmptyList confirms the lookup is safe on an
// empty list.
func TestOnNotificationList_EmptyList(t *testing.T) {
	saved := notificationList
	defer func() { notificationList = saved }()
	notificationList = nil

	if got := onNotificationList("anything"); got != -1 {
		t.Fatalf("onNotificationList on empty list = %d; want -1", got)
	}
}
