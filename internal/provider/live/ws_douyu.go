package live

import (
	"context"
	"encoding/binary"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func packDouyuSTT(msg string) []byte {
	payload := append([]byte(msg), 0)
	bodyLen := uint32(len(payload) + 8)
	pkt := make([]byte, 4+bodyLen)
	binary.LittleEndian.PutUint32(pkt[0:4], bodyLen)
	binary.LittleEndian.PutUint32(pkt[4:8], bodyLen)
	binary.LittleEndian.PutUint16(pkt[8:10], 689)
	binary.LittleEndian.PutUint16(pkt[10:12], 0)
	copy(pkt[12:], payload)
	return pkt
}

func (d *douyuAdapter) streamWS(ctx context.Context, roomID string, out chan<- ChatMessage) error {
	dialer := websocket.Dialer{HandshakeTimeout: 8 * time.Second}
	hdr := http.Header{
		"User-Agent": []string{biliUA},
		"Origin":     []string{"https://www.douyu.com"},
	}
	conn, _, err := dialer.DialContext(ctx, "wss://danmuproxy.douyu.com:8506/", hdr)
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	if err := conn.WriteMessage(websocket.BinaryMessage, packDouyuSTT("type@=loginreq/roomid@="+roomID+"/")); err != nil {
		return err
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, packDouyuSTT("type@=joingroup/rid@="+roomID+"/gid@=-9999/")); err != nil {
		return err
	}

	hbDone := make(chan struct{})
	defer close(hbDone)
	go func() {
		tk := time.NewTicker(40 * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-hbDone:
				return
			case <-tk.C:
				_ = conn.WriteMessage(websocket.BinaryMessage, packDouyuSTT("type@=mrkl/"))
			}
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		parseDouyuPackets(ctx, data, out)
	}
}

func parseDouyuPackets(ctx context.Context, data []byte, out chan<- ChatMessage) {
	for len(data) >= 12 {
		pktLen := int(binary.LittleEndian.Uint32(data[0:4]))
		if pktLen < 8 || 4+pktLen > len(data) {
			return
		}
		raw := string(data[12 : 4+pktLen])
		data = data[4+pktLen:]
		raw = strings.TrimRight(raw, "\x00")
		if !strings.Contains(raw, "type@=chatmsg/") {
			continue
		}
		fields := parseDouyuSTTMap(raw)
		txt := strings.TrimSpace(fields["txt"])
		if txt == "" {
			continue
		}
		select {
		case out <- ChatMessage{Author: fields["nn"], Text: txt, Color: 0xFFFFFF, Time: time.Now()}:
		case <-ctx.Done():
			return
		}
	}
}

func parseDouyuSTTMap(raw string) map[string]string {
	m := make(map[string]string)
	for _, part := range strings.Split(raw, "/") {
		k, v, ok := strings.Cut(part, "@=")
		if !ok {
			continue
		}
		v = strings.ReplaceAll(v, "@S", "/")
		v = strings.ReplaceAll(v, "@A", "@")
		m[k] = v
	}
	return m
}
