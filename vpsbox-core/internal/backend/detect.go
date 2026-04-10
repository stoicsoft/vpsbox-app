package backend

import (
	"context"
	"fmt"
	"runtime"
	"sort"
)

func Detect(ctx context.Context) (Backend, []string, error) {
	candidates := supportedBackends()
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Priority() < candidates[j].Priority() })

	attempted := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ok, err := candidate.Available(ctx)
		attempted = append(attempted, candidate.Name())
		if err != nil {
			continue
		}
		if ok {
			return candidate, attempted, nil
		}
	}

	if len(candidates) == 0 {
		return nil, attempted, fmt.Errorf("no backends are implemented for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	return candidates[0], attempted, nil
}

func supportedBackends() []Backend {
	backends := []Backend{}
	switch runtime.GOOS {
	case "darwin":
		backends = append(backends, NewMultipass(), NewLima())
	case "linux":
		backends = append(backends, NewMultipass(), NewLima())
	case "windows":
		backends = append(backends, NewMultipass())
	}

	return backends
}
