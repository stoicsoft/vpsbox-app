package backend

import (
	"fmt"
	"testing"
)

func TestIsAbsentInstance(t *testing.T) {
	absent := []error{
		fmt.Errorf(`multipass delete foo: delete failed: The following errors occurred:\ninstance "foo" does not exist`),
		fmt.Errorf(`instance 'webtop-test' does not exist`),
		fmt.Errorf("instance \"sc-sudoer\" is already deleted"),
	}
	for _, err := range absent {
		if !IsAbsentInstance(err) {
			t.Errorf("IsAbsentInstance(%q) = false, want true", err)
		}
	}

	present := []error{
		fmt.Errorf("multipass delete foo: failed to delete disk: file does not exist"),
		fmt.Errorf("permission denied"),
		nil,
	}
	for _, err := range present {
		if IsAbsentInstance(err) {
			t.Errorf("IsAbsentInstance(%v) = true, want false", err)
		}
	}
}
