package browser

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestReadTextLargeAndFragmented(t *testing.T) {
	large := bytes.Repeat([]byte("a"), 70_000)
	got := readFrames(t, func(w io.Writer) {
		if err := writeServerFrame(w, true, 0x1, large); err != nil {
			t.Error(err)
		}
	})
	if !bytes.Equal(got, large) {
		t.Fatalf("large frame len %d", len(got))
	}

	part1 := bytes.Repeat([]byte("b"), 40_000)
	part2 := bytes.Repeat([]byte("c"), 40_000)
	got = readFrames(t, func(w io.Writer) {
		if err := writeServerFrame(w, false, 0x1, part1); err != nil {
			t.Error(err)
		}
		if err := writeServerFrame(w, true, 0x0, part2); err != nil {
			t.Error(err)
		}
	})
	want := append(append([]byte{}, part1...), part2...)
	if !bytes.Equal(got, want) {
		t.Fatalf("fragment len %d", len(got))
	}
}

func readFrames(t *testing.T, write func(io.Writer)) []byte {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	errCh := make(chan error, 1)
	go func() {
		errCh <- func() error {
			write(server)
			return nil
		}()
	}()
	w := &wsConn{c: client, r: bufio.NewReader(client)}
	got, err := w.readText(time.Now().Add(2 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	return got
}

func writeServerFrame(w io.Writer, fin bool, opcode byte, payload []byte) error {
	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	n := len(payload)
	hdr := []byte{b0}
	switch {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n < 65536:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(n))
		hdr = append(hdr, 127)
		hdr = append(hdr, buf[:]...)
	}
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
