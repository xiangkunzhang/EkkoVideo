package live

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Minimal Tars/JCE encoder & decoder for Huya WebSocket chat notices (iUri=1400).
func tarsWriteHead(buf *bytes.Buffer, typ byte, tag int) {
	if tag < 15 {
		buf.WriteByte(byte(tag<<4) | (typ & 0x0f))
	} else {
		buf.WriteByte(0xf0 | (typ & 0x0f))
		buf.WriteByte(byte(tag))
	}
}

func tarsWriteInt64(buf *bytes.Buffer, tag int, v int64) {
	switch {
	case v == 0:
		tarsWriteHead(buf, 12, tag)
	case v >= -128 && v <= 127:
		tarsWriteHead(buf, 0, tag)
		buf.WriteByte(byte(int8(v)))
	case v >= -32768 && v <= 32767:
		tarsWriteHead(buf, 1, tag)
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], uint16(int16(v)))
		buf.Write(b[:])
	case v >= -2147483648 && v <= 2147483647:
		tarsWriteHead(buf, 2, tag)
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(int32(v)))
		buf.Write(b[:])
	default:
		tarsWriteHead(buf, 3, tag)
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(v))
		buf.Write(b[:])
	}
}

func tarsWriteString(buf *bytes.Buffer, tag int, s string) {
	if len(s) <= 255 {
		tarsWriteHead(buf, 6, tag)
		buf.WriteByte(byte(len(s)))
	} else {
		tarsWriteHead(buf, 7, tag)
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(len(s)))
		buf.Write(b[:])
	}
	buf.WriteString(s)
}

func tarsWriteSimpleList(buf *bytes.Buffer, tag int, data []byte) {
	tarsWriteHead(buf, 13, tag)
	tarsWriteHead(buf, 0, 0)
	tarsWriteInt64(buf, 0, int64(len(data)))
	buf.Write(data)
}

func packHuyaRegisterGroup(uid int64) []byte {
	var grp bytes.Buffer
	tarsWriteHead(&grp, 9, 0) // vector<string> vGroupId
	tarsWriteInt64(&grp, 0, 2)
	tarsWriteString(&grp, 0, "live:"+strconv.FormatInt(uid, 10))
	tarsWriteString(&grp, 0, "chat:"+strconv.FormatInt(uid, 10))
	tarsWriteString(&grp, 1, "") // sToken

	var outer bytes.Buffer
	tarsWriteInt64(&outer, 0, 16) // WSRegisterGroupReq
	tarsWriteSimpleList(&outer, 1, grp.Bytes())
	return outer.Bytes()
}

func packHuyaHeartbeat() []byte {
	var outer bytes.Buffer
	tarsWriteInt64(&outer, 0, 1) // ping
	return outer.Bytes()
}

func (h *huyaAdapter) streamWS(ctx context.Context, roomID string, out chan<- ChatMessage) error {
	var res struct {
		Data struct {
			ProfileInfo struct {
				UID int64 `json:"uid"`
			} `json:"profileInfo"`
		} `json:"data"`
	}
	u := "https://mp.huya.com/cache.php?m=Live&do=profileRoom&roomid=" + url.QueryEscape(roomID)
	_ = h.getJSON(ctx, u, &res)
	uid := res.Data.ProfileInfo.UID
	if uid <= 0 {
		uid, _ = strconv.ParseInt(roomID, 10, 64)
	}
	if uid <= 0 {
		<-ctx.Done()
		return nil
	}

	dialer := websocket.Dialer{HandshakeTimeout: 8 * time.Second}
	hdr := http.Header{
		"User-Agent": []string{biliUA},
		"Origin":     []string{"https://www.huya.com"},
	}
	conn, _, err := dialer.DialContext(ctx, "wss://cdnws.api.huya.com/", hdr)
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	if err := conn.WriteMessage(websocket.BinaryMessage, packHuyaRegisterGroup(uid)); err != nil {
		return err
	}

	hbDone := make(chan struct{})
	defer close(hbDone)
	go func() {
		tk := time.NewTicker(45 * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-hbDone:
				return
			case <-tk.C:
				_ = conn.WriteMessage(websocket.BinaryMessage, packHuyaHeartbeat())
			}
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		parseHuyaTarsFrame(ctx, data, out)
	}
}

func parseHuyaTarsFrame(ctx context.Context, data []byte, out chan<- ChatMessage) {
	fields := tarsDecodeTop(data)
	cmdType, _ := fields[0].(int64)
	if cmdType != 7 && cmdType != 2 {
		return
	}
	inner, ok := fields[1].([]byte)
	if !ok || len(inner) == 0 {
		return
	}
	pushFields := tarsDecodeTop(inner)
	uri, _ := pushFields[1].(int64)
	if uri != 1400 {
		return
	}
	msgBytes, ok := pushFields[2].([]byte)
	if !ok || len(msgBytes) == 0 {
		return
	}
	msgFields := tarsDecodeTop(msgBytes)
	txt, _ := msgFields[3].(string)
	txt = strings.TrimSpace(txt)
	if txt == "" {
		return
	}
	author := ""
	if uBytes, ok := msgFields[0].([]byte); ok {
		uf := tarsDecodeTop(uBytes)
		author, _ = uf[2].(string)
	}
	select {
	case out <- ChatMessage{Author: author, Text: txt, Color: 0xFFFFFF, Time: time.Now()}:
	case <-ctx.Done():
	}
}

// tarsDecodeTop decodes top-level tag -> value (int64, string, or []byte for struct/simpleList).
func tarsDecodeTop(data []byte) map[int]any {
	out := make(map[int]any)
	i := 0
	readInt := func(typ byte) (int64, bool) {
		switch typ {
		case 12:
			return 0, true
		case 0:
			if i >= len(data) {
				return 0, false
			}
			v := int64(int8(data[i]))
			i++
			return v, true
		case 1:
			if i+2 > len(data) {
				return 0, false
			}
			v := int64(int16(binary.BigEndian.Uint16(data[i : i+2])))
			i += 2
			return v, true
		case 2:
			if i+4 > len(data) {
				return 0, false
			}
			v := int64(int32(binary.BigEndian.Uint32(data[i : i+4])))
			i += 4
			return v, true
		case 3:
			if i+8 > len(data) {
				return 0, false
			}
			v := int64(binary.BigEndian.Uint64(data[i : i+8]))
			i += 8
			return v, true
		}
		return 0, false
	}

	for i < len(data) {
		b := data[i]
		i++
		typ := b & 0x0f
		tag := int(b >> 4)
		if tag == 15 {
			if i >= len(data) {
				break
			}
			tag = int(data[i])
			i++
		}
		switch typ {
		case 0, 1, 2, 3, 12:
			v, ok := readInt(typ)
			if !ok {
				return out
			}
			out[tag] = v
		case 4:
			i += 4
		case 5:
			i += 8
		case 6:
			if i >= len(data) {
				return out
			}
			l := int(data[i])
			i++
			if i+l > len(data) {
				return out
			}
			out[tag] = string(data[i : i+l])
			i += l
		case 7:
			if i+4 > len(data) {
				return out
			}
			l := int(binary.BigEndian.Uint32(data[i : i+4]))
			i += 4
			if l < 0 || i+l > len(data) {
				return out
			}
			out[tag] = string(data[i : i+l])
			i += l
		case 10:
			// Struct begin: capture until matching struct end (typ 11)
			start := i
			depth := 1
			for i < len(data) && depth > 0 {
				sb := data[i]
				i++
				styp := sb & 0x0f
				stag := int(sb >> 4)
				if stag == 15 && i < len(data) {
					i++
				}
				switch styp {
				case 0:
					i++
				case 1, 4:
					if styp == 1 {
						i += 2
					} else {
						i += 4
					}
				case 2:
					i += 4
				case 3, 5:
					i += 8
				case 6:
					if i < len(data) {
						i += 1 + int(data[i])
					}
				case 7:
					if i+4 <= len(data) {
						i += 4 + int(binary.BigEndian.Uint32(data[i:i+4]))
					}
				case 10:
					depth++
				case 11:
					depth--
				case 12:
				case 13:
					if i < len(data) {
						i++ // inner type
						if i < len(data) {
							lb := data[i] & 0x0f
							i++
							ln, _ := readInt(lb)
							i += int(ln)
						}
					}
				default:
					return out
				}
			}
			if i <= len(data) {
				end := i
				if end > start {
					end--
				}
				out[tag] = data[start:end]
			}
		case 11:
			return out
		case 13:
			if i+1 >= len(data) {
				return out
			}
			i++ // element type (0=byte)
			hb := data[i]
			i++
			ln, ok := readInt(hb & 0x0f)
			if !ok || ln < 0 || i+int(ln) > len(data) {
				return out
			}
			out[tag] = data[i : i+int(ln)]
			i += int(ln)
		default:
			return out
		}
	}
	return out
}
