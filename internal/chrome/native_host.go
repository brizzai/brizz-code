package chrome

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/brizzai/fleet/internal/debuglog"
)

// RunNativeHost runs the native messaging host process.
// It creates a Unix socket and bridges between socket clients and Chrome's stdio.
func RunNativeHost() {
	log := debuglog.Logger

	sockPath := SocketPath()

	// Remove stale socket if it exists.
	if _, err := os.Stat(sockPath); err == nil {
		os.Remove(sockPath)
	}

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		log.Error("chrome-host: failed to create socket", "err", err)
		os.Exit(1)
	}
	defer listener.Close()

	// Set socket permissions.
	os.Chmod(sockPath, 0600)

	// Clean up socket on exit.
	cleanup := func() {
		listener.Close()
		os.Remove(sockPath)
	}
	defer cleanup()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cleanup()
		os.Exit(0)
	}()

	log.Info("chrome-host: listening", "socket", sockPath)

	// Pending responses from Chrome, keyed by command ID.
	var mu sync.Mutex
	pending := make(map[string]chan *Response)

	// Read responses from Chrome (stdin) in a goroutine.
	go func() {
		for {
			msg, err := readNativeMessage(os.Stdin)
			if err != nil {
				log.Error("chrome-host: stdin read error", "err", err)
				cleanup()
				os.Exit(0)
			}

			var resp Response
			if err := json.Unmarshal(msg, &resp); err != nil {
				log.Warn("chrome-host: bad response JSON", "err", err)
				continue
			}

			mu.Lock()
			if ch, ok := pending[resp.ID]; ok {
				ch <- &resp
				delete(pending, resp.ID)
			}
			mu.Unlock()
		}
	}()

	// Chrome native messaging stdout writes must be serialized.
	var writeMu sync.Mutex

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Debug("chrome-host: accept error", "err", err)
			continue
		}

		go handleSocketClient(conn, &mu, pending, &writeMu)
	}
}

func handleSocketClient(conn net.Conn, mu *sync.Mutex, pending map[string]chan *Response, writeMu *sync.Mutex) {
	defer conn.Close()

	// Read the full command from socket.
	data, err := io.ReadAll(conn)
	if err != nil {
		return
	}

	var cmd Command
	if err := json.Unmarshal(data, &cmd); err != nil {
		resp := Response{Success: false, Error: "invalid command JSON"}
		respData, _ := json.Marshal(resp)
		conn.Write(respData)
		return
	}

	if cmd.ID == "" {
		cmd.ID = fmt.Sprintf("%d", os.Getpid())
	}

	// Register pending response channel.
	respCh := make(chan *Response, 1)
	mu.Lock()
	pending[cmd.ID] = respCh
	mu.Unlock()

	// Forward command to Chrome via stdout (native messaging format).
	cmdData, _ := json.Marshal(cmd)
	writeMu.Lock()
	err = writeNativeMessage(os.Stdout, cmdData)
	writeMu.Unlock()

	if err != nil {
		mu.Lock()
		delete(pending, cmd.ID)
		mu.Unlock()
		resp := Response{ID: cmd.ID, Success: false, Error: "failed to write to Chrome"}
		respData, _ := json.Marshal(resp)
		conn.Write(respData)
		return
	}

	// Wait for response from Chrome (with timeout handled by client).
	resp := <-respCh
	respData, _ := json.Marshal(resp)
	conn.Write(respData)
}

// readNativeMessage reads a length-prefixed native messaging message.
// Format: 4-byte LE uint32 length + JSON bytes.
func readNativeMessage(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	if length > 1024*1024 { // 1MB sanity limit
		return nil, fmt.Errorf("message too large: %d bytes", length)
	}
	msg := make([]byte, length)
	if _, err := io.ReadFull(r, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// writeNativeMessage writes a length-prefixed native messaging message.
func writeNativeMessage(w io.Writer, data []byte) error {
	length := uint32(len(data))
	if err := binary.Write(w, binary.LittleEndian, length); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}
