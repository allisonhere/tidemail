package ui

import (
	"context"
	"reflect"
	"testing"
)

func TestChooseNotificationWindow(t *testing.T) {
	for _, tc := range []struct {
		name      string
		clients   []notificationClient
		ancestors []int
		want      string
	}{
		{"terminal ancestor", []notificationClient{{Address: "0xabc", PID: 20}}, []int{30, 20, 10}, "0xabc"},
		{"other app", []notificationClient{{Address: "0xabc", PID: 99}}, []int{30, 20}, ""},
		{"nearest ancestor", []notificationClient{{Address: "0xabc", PID: 20}, {Address: "0xdef", PID: 10}}, []int{30, 20, 10}, "0xabc"},
		{"terminal server focused window", []notificationClient{{Address: "0xabc", PID: 20, FocusHistoryID: 2}, {Address: "0xdef", PID: 20, FocusHistoryID: 0}}, []int{30, 20}, "0xdef"},
		{"ambiguous background server", []notificationClient{{Address: "0xabc", PID: 20, FocusHistoryID: 2}, {Address: "0xdef", PID: 20, FocusHistoryID: 1}}, []int{30, 20}, ""},
		{"invalid address", []notificationClient{{Address: "0xabc;exec bad", PID: 20}}, []int{30, 20}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := chooseNotificationWindow(tc.clients, tc.ancestors); got != tc.want {
				t.Fatalf("address=%q want %q", got, tc.want)
			}
		})
	}
}

func TestFocusNotificationWindowDispatchers(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		var calls [][]string
		err := focusNotificationWindow(context.Background(), "0xabc", func(_ context.Context, args ...string) ([]byte, error) {
			calls = append(calls, args)
			if legacy && len(calls) == 1 {
				return []byte("Invalid dispatcher"), nil
			}
			return []byte("ok\n"), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		want := [][]string{{"dispatch", `hl.dsp.focus({ window = "address:0xabc" })`}}
		if legacy {
			want = append(want, []string{"dispatch", "focuswindow", "address:0xabc"})
		}
		if !reflect.DeepEqual(calls, want) {
			t.Fatalf("calls = %#v want %#v", calls, want)
		}
	}
}
