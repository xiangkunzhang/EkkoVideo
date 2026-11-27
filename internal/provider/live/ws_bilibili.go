package live

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func packBiliPacket(op uint32, body []byte) []byte {
	total := uint32(16 + len(body))
	pkt := make([]byte, total)
	binary.BigEndian.PutUint32(pkt[0:4], total)
	binary.BigEndian.PutUint16(pkt[4:6], 16)
	binary.BigEndian.PutUint16(pkt[6:8], 1)
	binary.BigEndian.PutUint32(pkt[8:12], op)
	binary.BigEndian.PutUint32(pkt[12:16], 1)
	copy(pkt[16:], body)
	return pkt
}

func (b *bilibiliAdapter) streamWS(ctx context.Context, roomID string, out chan<- ChatMessage) error {
	rid, err := strconv.ParseInt(roomID, 10, 64)
	if err != nil || rid <= 0 {
		return fmt.Errorf("bilibili ws: invalid room %q", roomID)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 8 * time.Second}
	hdr := http.Header{
		"User-Agent": []string{biliUA},
		"Origin":     []string{"https://live.bilibili.com"},
	}
	conn, _, err := dialer.DialContext(ctx, "wss://broadcastlv.chat.bilibili.com:443/sub", hdr)
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	auth, _ := json.Marshal(map[string]any{
		"uid":      0,
		"roomid":   rid,
		"protover": 2,
		"platform": "web",
		"type":     2,
	})
	if err := conn.WriteMessage(websocket.BinaryMessage, packBiliPacket(7, auth)); err != nil {
		return err
	}

	hbDone := make(chan struct{})
	defer close(hbDone)
	go func() {
		tk := time.NewTicker(30 * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-hbDone:
				return
			case <-tk.C:
				_ = conn.WriteMessage(websocket.BinaryMessage, packBiliPacket(2, []byte("[object Object]")))
			}
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		parseBiliFrame(ctx, msg, out)
	}
}

func parseBiliFrame(ctx context.Context, data []byte, out chan<- ChatMessage) {
	for len(data) >= 16 {
		total := int(binary.BigEndian.Uint32(data[0:4]))
		hdrLen := int(binary.BigEndian.Uint16(data[4:6]))
		ver := binary.BigEndian.Uint16(data[6:8])
		op := binary.BigEndian.Uint32(data[8:12])
		if total < 16 || total > len(data) || hdrLen < 16 || hdrLen > total {
			return
		}
		body := data[hdrLen:total]
		data = data[total:]

		if op != 5 {
			continue
		}
		if ver == 2 {
			zr, err := zlib.NewReader(bytes.NewReader(body))
			if err == nil {
				unpacked, err := io.ReadAll(io.LimitReader(zr, 4<<20))
				_ = zr.Close()
				if err == nil {
					parseBiliFrame(ctx, unpacked, out)
				}
			}
			continue
		}
		var env struct {
			Cmd  string            `json:"cmd"`
			Info []json.RawMessage `json:"info"`
		}
		if json.Unmarshal(body, &env) != nil {
			continue
		}
		if !strings.HasPrefix(env.Cmd, "DANMU_MSG") || len(env.Info) < 3 {
			continue
		}
		var text string
		if json.Unmarshal(env.Info[1], &text) != nil || strings.TrimSpace(text) == "" {
			continue
		}
		var userArr []json.RawMessage
		var author string
		if json.Unmarshal(env.Info[2], &userArr) == nil && len(userArr) >= 2 {
			_ = json.Unmarshal(userArr[1], &author)
		}
		var metaArr []json.RawMessage
		color := uint32(0xFFFFFF)
		if json.Unmarshal(env.Info[0], &metaArr) == nil && len(metaArr) >= 4 {
			var c uint32
			if json.Unmarshal(metaArr[3], &c) == nil && c > 0 {
				color = c
			}
		}
		select {
		case out <- ChatMessage{Author: author, Text: text, Color: color, Time: time.Now()}:
		case <-ctx.Done():
			return
		}
	}
}
