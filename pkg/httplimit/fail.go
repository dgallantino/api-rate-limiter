package httplimit

import (
	"fmt"
	"strings"
)

type FailMode int

const (
	FailClosed FailMode = iota
	FailOpen
)

func ParseFail(s string) (FailMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "closed":
		return FailClosed, nil
	case "open":
		return FailOpen, nil
	default:
		return 0, fmt.Errorf("httplimit: fail must be open or closed")
	}
}
