package speedtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/hashicorp/go-hclog"

	"github.com/hashicorp/go-multierror"
)

type TestResults struct {
	Ping       Ping    `json:"ping"`
	Download   Speed   `json:"download"`
	Upload     Speed   `json:"upload"`
	PacketLoss float64 `json:"packetLoss"`
	ISP        string  `json:"isp"`
	Server     Server  `json:"server"`
}

func (tr TestResults) IsMissingData() bool {
	return tr.Ping.IsMissingData() ||
		tr.Download.IsMissingData() ||
		tr.Upload.IsMissingData() ||
		// Packet loss can be zero
		tr.Server.IsMissingData() ||
		tr.ISP == ""
}

type Ping struct {
	Jitter  float64 `json:"jitter"`
	Latency float64 `json:"latency"`
}

func (p Ping) IsZero() bool {
	return p.Latency == 0 && p.Jitter == 0
}

func (p Ping) IsMissingData() bool {
	return p.Latency == 0
}

type Speed struct {
	Bandwidth float64 `json:"bandwidth"`
	Bytes     float64 `json:"bytes"`
	Elapsed   float64 `json:"elapsed"`
}

func (s Speed) IsZero() bool {
	return s.Bandwidth == 0 && s.Bytes == 0 && s.Elapsed == 0
}

func (s Speed) IsMissingData() bool {
	return s.Bandwidth == 0 || s.Bytes == 0 || s.Elapsed == 0
}

type Server struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Country  string `json:"country"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

func (s Server) IsZero() bool {
	return s.ID == 0 &&
		s.Name == "" &&
		s.Location == "" &&
		s.Country == "" &&
		s.Host == "" &&
		s.Port == 0
}

func (s Server) IsMissingData() bool {
	return s.ID == 0 ||
		s.Name == "" ||
		s.Location == "" ||
		s.Country == "" ||
		s.Host == "" ||
		s.Port == 0
}

func RunTests(ctx context.Context, log hclog.Logger, cmd Command, serverIDs ...int) ([]TestResults, error) {
	results := make([]TestResults, 0, len(serverIDs))
	merr := new(multierror.Error)
	for _, serverID := range serverIDs {
		if isClosed(ctx) {
			return results, merr.ErrorOrNil()
		}
		// Don't fail immediately if one server errors. This gives us the most information we can out of the set
		result, err := RunTest(log, cmd, serverID)
		merr = multierror.Append(merr, err)
		results = append(results, result)
	}
	return results, merr.ErrorOrNil()
}

func isClosed(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func RunTest(log hclog.Logger, cmd Command, serverID int) (TestResults, error) {
	log.Info("Running speed test",
		"server_id", serverID,
	)
	args := append(cmd.Args, "-s", strconv.Itoa(serverID))
	c := exec.Command(cmd.Name, args...)

	// Ignoring stderr
	stdout := new(bytes.Buffer)
	c.Stdout = stdout

	err := c.Run()
	if err != nil {
		return TestResults{}, fmt.Errorf("failed to run speed test: %w", err)
	}

	results := TestResults{}
	err = json.Unmarshal(stdout.Bytes(), &results)
	if err != nil {
		return TestResults{}, fmt.Errorf("failed to unmarshal results: %w", err)
	}

	log.Info("Results", "full_results", string(stdout.Bytes()))

	return results, nil
}
