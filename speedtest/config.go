package speedtest

import (
	"fmt"
	"strings"
	"time"
)

type Config struct {
	Command Command `json:"command"`

	// Servers list to query
	Servers []int `json:"servers"`

	// TickRate how often to run the speed tests. This should be greater than the
	// sum of time it takes to run all of the tests across all servers
	TickRate jsonDuration `json:"tick"`
}

type Command struct {
	Name string
	Args []string
}

type jsonDuration struct {
	time.Duration
}

func (d jsonDuration) MarshalJSON() ([]byte, error) {
	return []byte(d.String()), nil
}

func (d jsonDuration) String() string {
	return d.Duration.String()
}

func (d jsonDuration) GoString() string {
	return d.String()
}

func (d *jsonDuration) UnmarshalJSON(b []byte) error {
	str := string(b)
	if !strings.HasPrefix(str, `"`) {
		return fmt.Errorf("invalid duration1: %s", str)
	}
	if !strings.HasSuffix(str, `"`) {
		return fmt.Errorf("invalid duration2: %s", str)
	}
	str = strings.Trim(str, `"`)

	dur, err := time.ParseDuration(string(str))
	if err != nil {
		return err
	}
	d.Duration = dur
	return nil
}
