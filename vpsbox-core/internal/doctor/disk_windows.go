//go:build windows

package doctor

import "errors"

func freeDiskGB(path string) (float64, error) {
	return 0, errors.New("not implemented on windows")
}
