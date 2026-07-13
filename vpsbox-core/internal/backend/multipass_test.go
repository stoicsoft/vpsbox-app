package backend

import (
	"reflect"
	"testing"
)

func TestMultipassRestoreArgsAreNonInteractive(t *testing.T) {
	want := []string{"restore", "--destructive", "demo.clean-base"}
	if got := multipassRestoreArgs("demo", "clean-base"); !reflect.DeepEqual(got, want) {
		t.Fatalf("multipassRestoreArgs() = %#v, want %#v", got, want)
	}
}
