package chrome

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client communicates with the Chrome extension via the native host's Unix socket.
type Client struct{}

// Send sends a command to the Chrome extension and waits for a response.
// Returns an error if the socket doesn't exist (extension not running).
func (c *Client) Send(cmd *Command) (*Response, error) {
	sockPath := SocketPath()

	conn, err := net.DialTimeout("unix", sockPath, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("chrome extension not available: %w", err)
	}
	defer conn.Close()

	// Set read deadline for response.
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send command.
	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("marshal command: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		return nil, fmt.Errorf("write command: %w", err)
	}

	// Signal we're done writing so the host can ReadAll.
	if uc, ok := conn.(*net.UnixConn); ok {
		uc.CloseWrite()
	}

	// Read response.
	buf := make([]byte, 64*1024)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if !resp.Success {
		return &resp, fmt.Errorf("chrome: %s", resp.Error)
	}

	return &resp, nil
}
