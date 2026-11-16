package browser

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// maxWSMessage is large enough for a CDP screenshot of a 2MB PNG.
const maxWSMessage = 8 << 20

type wsConn struct {
	c net.Conn
	r *bufio.Reader
}

func dialWS(raw string) (*wsConn, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	conn, err := net.DialTimeout("tcp", host, 5*time.Second)
	if err != nil {
		return nil, err
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", path, host, key)
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(status, "101") {
		conn.Close()
		return nil, fmt.Errorf("websocket upgrade: %s", strings.TrimSpace(status))
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	want := base64.StdEncoding.EncodeToString(sum[:])
	var accept string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		if line == "\r\n" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "sec-websocket-accept:") {
			accept = strings.TrimSpace(line[len("sec-websocket-accept:"):])
		}
	}
	if accept != want {
		conn.Close()
		return nil, fmt.Errorf("websocket accept mismatch")
	}
	return &wsConn{c: conn, r: br}, nil
}

func (w *wsConn) Close() error {
	if w == nil || w.c == nil {
		return nil
	}
	return w.c.Close()
}

func (w *wsConn) writeText(payload []byte) error {
	header := []byte{0x81}
	n := len(payload)
	switch {
	case n < 126:
		header = append(header, byte(n)|0x80)
	case n < 65536:
		header = append(header, 126|0x80, byte(n>>8), byte(n))
	default:
		return fmt.Errorf("websocket frame too large")
	}
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header = append(header, mask...)
	masked := make([]byte, n)
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := w.c.Write(header); err != nil {
		return err
	}
	_, err := w.c.Write(masked)
	return err
}

func (w *wsConn) readText(deadline time.Time) ([]byte, error) {
	var msg []byte
	started := false
	for {
		fin, opcode, payload, err := w.readFrame(deadline)
		if err != nil {
			return nil, err
		}
		switch opcode {
		case 0x1:
			msg = append(msg[:0], payload...)
			started = true
		case 0x0:
			if !started {
				return nil, fmt.Errorf("websocket continuation without a text frame")
			}
			msg = append(msg, payload...)
		case 0x8:
			return nil, io.EOF
		case 0x9:
			if len(payload) > 125 {
				return nil, fmt.Errorf("websocket ping too large")
			}
			pong := []byte{0x8A, byte(len(payload))}
			if _, err := w.c.Write(append(pong, payload...)); err != nil {
				return nil, err
			}
			continue
		default:
			continue
		}
		if len(msg) > maxWSMessage {
			return nil, fmt.Errorf("websocket message too large")
		}
		if fin {
			return msg, nil
		}
	}
}

func (w *wsConn) readFrame(deadline time.Time) (bool, byte, []byte, error) {
	if err := w.c.SetReadDeadline(deadline); err != nil {
		return false, 0, nil, err
	}
	b0, err := w.r.ReadByte()
	if err != nil {
		return false, 0, nil, err
	}
	b1, err := w.r.ReadByte()
	if err != nil {
		return false, 0, nil, err
	}
	length := int(b1 & 0x7f)
	switch length {
	case 126:
		var buf [2]byte
		if _, err := io.ReadFull(w.r, buf[:]); err != nil {
			return false, 0, nil, err
		}
		length = int(binary.BigEndian.Uint16(buf[:]))
	case 127:
		var buf [8]byte
		if _, err := io.ReadFull(w.r, buf[:]); err != nil {
			return false, 0, nil, err
		}
		n := binary.BigEndian.Uint64(buf[:])
		if n > maxWSMessage {
			return false, 0, nil, fmt.Errorf("websocket frame too large")
		}
		length = int(n)
	}
	if length > maxWSMessage {
		return false, 0, nil, fmt.Errorf("websocket frame too large")
	}
	if b1&0x80 != 0 {
		if _, err := io.CopyN(io.Discard, w.r, 4); err != nil {
			return false, 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(w.r, payload); err != nil {
		return false, 0, nil, err
	}
	return b0&0x80 != 0, b0 & 0x0f, payload, nil
}
