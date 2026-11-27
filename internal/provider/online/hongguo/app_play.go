package hongguo

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/bits"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiangkunzhang/Velan/internal/provider/online/upstream"
)

var appPlayHosts = []string{
	// v5 播放接口在官方客户端里固定走 api5-normal-sinfonlinea，放在首位。
	"api5-normal-sinfonlinea.fqnovel.com",
	"api3-normal-sinfonlinea.fqnovel.com",
	"api5-normal-sinfonlineb.fqnovel.com",
	"api5-normal-hl.fqnovel.com",
	"api5-normal-lf.fqnovel.com",
}

type appSession struct {
	mu     sync.Mutex
	dev    *RegisteredDevice
	signer *AppSigner
	// retryAfter 是身份恢复失败后的冷却截止时间：冷却期内不重复注册设备。
	retryAfter time.Time
	// emptyRefreshAfter 限制空包触发的身份轮换频率（30s 冷却，防连续空包打爆注册接口）。
	emptyRefreshAfter time.Time
	// register 是设备注册入口，测试可以替换掉，免得真的打上游。
	register func(*http.Client) (*RegisteredDevice, error)
}

var defaultAppSession = &appSession{}

// registerDevice 走可替换的注册入口。
func (s *appSession) registerDevice(httpClient *http.Client) (*RegisteredDevice, error) {
	if s.register != nil {
		return s.register(httpClient)
	}
	return RegisterDevice(httpClient)
}

// get 返回可用的设备身份与签名器。
//
// speculative=true 表示这是预加载一类的后台请求：它可以复用已有身份，但不会
// 因为身份缺失而去注册新设备 —— 注册是重操作，抢在用户真正要看之前发生
// 没有意义，还会和前台播放抢同一把锁。
func (s *appSession) get(httpClient *http.Client, speculative bool) (*RegisteredDevice, *AppSigner, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dev != nil && s.signer != nil {
		return s.dev, s.signer, nil
	}
	if cached := loadCachedDevice(); cached != nil {
		s.dev = cached
		s.signer = NewAppSigner()
		return s.dev, s.signer, nil
	}
	// 需要新建身份。冷却期内不重复注册：注册接口会被上游风控。
	if !s.retryAfter.IsZero() && time.Now().Before(s.retryAfter) {
		return nil, nil, fmt.Errorf("播放连接恢复失败，请稍后重试")
	}
	if speculative {
		return nil, nil, fmt.Errorf("预加载不注册新设备身份")
	}
	dev, err := s.registerDevice(httpClient)
	if err != nil {
		// 注册失败也要冷却：否则同一次取流里的第二次尝试会立刻再注册一次，
		// 一次失败就变成两次注册。注册接口被连续打会被上游风控。
		s.retryAfter = time.Now().Add(identityCooldown)
		return nil, nil, err
	}
	saveCachedDevice(dev)
	s.dev = dev
	s.signer = NewAppSigner()
	return s.dev, s.signer, nil
}

// DeriveSpadeKey decodes a spade_a string into a 16-byte CENC AES-128-CTR key.
func DeriveSpadeKey(spadeA string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(spadeA))
	if err != nil {
		return nil, err
	}
	if len(raw) < 3 {
		return nil, fmt.Errorf("spade_a too short")
	}
	length := len(raw) - int(raw[0]^raw[1]^raw[2]) + 47
	if length > len(raw)-1 {
		length = len(raw) - 1
	}
	if length < 33 {
		return nil, fmt.Errorf("spade_a length invalid: %d", length)
	}
	work := append([]byte(nil), raw[1:1+length]...)
	a, b := 85, 246
	for i := range work {
		old := int(work[i])
		prev := b
		if i&1 != 0 {
			prev = a
			a = old
		} else {
			b = old
		}
		work[i] = byte(-21 - bits.OnesCount(uint(i)) + (prev ^ old))
	}
	key, err := hex.DecodeString(string(work[1:33]))
	if err != nil || len(key) != 16 {
		return nil, fmt.Errorf("spade_a hex decode failed")
	}
	return key, nil
}

func decryptSpadeURL(encURL string, seed []byte) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encURL))
	if err != nil {
		return "", err
	}
	if len(raw) < 20 || raw[0] != 0xa8 || raw[2] != 1 || raw[3] != 0 {
		return "", fmt.Errorf("invalid spade url header")
	}
	salt, _ := hex.DecodeString(ttKDFSaltHex)
	h1 := sha512.Sum512(seed)
	h := sha512.Sum512(append(h1[:], salt...))
	blk, err := aes.NewCipher(h[:16])
	if err != nil {
		return "", err
	}
	ctLen := ((len(raw) - 4) / 16) * 16
	plain := make([]byte, ctLen)
	cipher.NewCBCDecrypter(blk, h[16:32]).CryptBlocks(plain, raw[4:4+ctLen])
	end := len(plain)
	if end > 0 {
		pad := int(plain[end-1])
		if pad >= 1 && pad <= 16 && pad <= end {
			end -= pad
		}
	}
	for end > 0 && plain[end-1] == 0 {
		end--
	}
	return string(plain[:end]), nil
}

// AppStream is a resolved episode stream from the fqnovel App API.
type AppStream struct {
	Definition string
	Codec      string
	Width      int
	Height     int
	URL        string
	BackupURL  string
	Key        []byte
}

// AppResolve resolves any episode (including ep 6+) via the signed fqnovel App API.
func (c *Client) AppResolve(itemID, definition string) (*AppStream, error) {
	return c.appResolve(itemID, definition, false)
}

// AppResolveSpeculative is the prefetch-side variant: it may reuse an existing
// device identity but never registers a new one.
func (c *Client) AppResolveSpeculative(itemID, definition string) (*AppStream, error) {
	return c.appResolve(itemID, definition, true)
}

// appResolve 取流。
//
// 身份被上游拒绝时（业务码 10001 / 10002 / 110001），换一个全新的设备身份
// 重试一次；换完还被拒就放弃并进入冷却，不做无限重注册。
//
// 普通失败（网络 / 节点 / 协议）**保留身份**重试。重新注册设备是上游会做
// 风控的动作：一旦失败就换身份，等于把一次网络抖动放大成注册风暴，反而
// 让后续所有请求都拿不到流。
func (c *Client) appResolve(itemID, definition string, speculative bool) (*AppStream, error) {
	if c.HTTP == nil {
		return nil, fmt.Errorf("nil http client")
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		dev, signer, err := defaultAppSession.get(c.HTTP, speculative)
		if err != nil {
			// 注册失败不是身份被拒：保留现状（下次仍可复用已有身份），
			// 绝不在这里 reset —— 那会删掉身份并触发新一轮注册。
			lastErr = err
			continue
		}
		st, err := c.resolveWithIdentity(dev, signer, itemID, definition)
		if err == nil {
			return st, nil
		}
		lastErr = err

		if upstream.IsIdentityRejected(err) || isEmptyPlaybackErr(err) {
			// 预加载不碰身份：身份问题留给前台请求处理。
			if speculative {
				return nil, err
			}
			if isEmptyPlaybackErr(err) && !upstream.IsIdentityRejected(err) {
				defaultAppSession.mu.Lock()
				inCooldown := !defaultAppSession.emptyRefreshAfter.IsZero() && time.Now().Before(defaultAppSession.emptyRefreshAfter)
				if !inCooldown {
					defaultAppSession.emptyRefreshAfter = time.Now().Add(30 * time.Second)
				}
				defaultAppSession.mu.Unlock()
				if inCooldown {
					return nil, err
				}
			}
			dev2, signer2, rerr := defaultAppSession.refreshRejected(c.HTTP, dev)
			if rerr != nil {
				return nil, rerr
			}
			st2, err2 := c.resolveWithIdentity(dev2, signer2, itemID, definition)
			if err2 == nil {
				return st2, nil
			}
			if upstream.IsIdentityRejected(err2) || isEmptyPlaybackErr(err2) {
				defaultAppSession.rejectReplacement(dev2)
			}
			return nil, err2
		}

		// 普通失败：身份不是问题，保留身份继续重试。
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("app resolve failed")
	}
	return nil, lastErr
}

func isEmptyPlaybackErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "empty response")
}

// resolveWithIdentity 用指定身份试一次取流。
func (c *Client) resolveWithIdentity(dev *RegisteredDevice, signer *AppSigner, itemID, definition string) (*AppStream, error) {
	// 路由优先级（对齐 hongguo-drama 8.1 & hongguoTV 0.16.14）：
	// 1. v5 GET video_tab/multi_video_model/v1/（当前命中率最高）
	// 2. v5 POST reading.snssdk.com/.../preload/v1 & fqnovel.com/.../player/v1
	// 3. v5 POST video_model/v1/（content_type=1004）
	// 4. 旧版 singleVideoModel 兜底
	if stTab, errTab := c.videoTabMultiVideoModel(dev, signer, itemID, definition); errTab == nil {
		return stTab, nil
	} else if upstream.IsIdentityRejected(errTab) {
		return nil, errTab
	}
	if st, err := c.multiVideoModelV5(dev, signer, itemID, definition); err == nil {
		return st, nil
	} else if upstream.IsIdentityRejected(err) {
		return nil, err
	}
	if stM, errM := c.v5VideoModelSingle(dev, signer, itemID, definition); errM == nil {
		return stM, nil
	} else if upstream.IsIdentityRejected(errM) {
		return nil, errM
	}
	st2, err2 := c.singleVideoModel(dev, itemID, definition)
	if err2 == nil {
		return st2, nil
	}
	if upstream.IsIdentityRejected(err2) {
		return nil, err2
	}
	return nil, err2
}

// v5VideoModelSingle 使用 v5 签名请求 /novel/player/video_model/v1/（content_type=1004）。
func (c *Client) v5VideoModelSingle(dev *RegisteredDevice, signer *AppSigner, itemID, definition string) (*AppStream, error) {
	if signer == nil {
		signer = NewAppSigner()
	}
	payload, err := json.Marshal(map[string]any{
		"biz_param": map[string]any{
			"detail_page_version":       0,
			"device_level":              3,
			"disable_digg_stat":         false,
			"from_video_id":             "",
			"need_all_video_definition": true,
			"need_mp4_align":            false,
			"source":                    9,
			"use_os_player":             false,
			"use_server_dns":            false,
			"video_platform":            3,
		},
		"content_type": 1004,
		"video_id":     itemID,
	})
	if err != nil {
		return nil, err
	}
	body, err := gzipBytes(payload)
	if err != nil {
		return nil, err
	}
	query := v5PlaybackQuery{
		DeviceID:    dev.DeviceID,
		InstallID:   dev.InstallID,
		CDID:        dev.CDID,
		VersionName: v5PreloadVersionName,
		Channel:     v5PreloadChannel,
		DeviceType:  v5PreloadDeviceType,
		OSVersion:   v5PreloadOSVersion,
	}
	hosts := []string{
		"api5-normal-sinfonlinea.fqnovel.com",
		"api5-normal-sinfonlinec.fqnovel.com",
	}
	anonClient := &http.Client{Timeout: c.HTTP.Timeout, Transport: c.HTTP.Transport}
	var lastErr error
	for _, host := range hosts {
		baseURL := "https://" + host + "/novel/player/video_model/v1/"
		fullURL, headers, err := signer.SignPlaybackV5(baseURL, query, body, currentMillis())
		if err != nil {
			lastErr = err
			continue
		}
		req, err := http.NewRequest(http.MethodPost, fullURL, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Content-Encoding", "gzip")
		resp, err := anonClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			continue
		}
		if len(raw) == 0 {
			lastErr = fmt.Errorf("v5 video_model empty response from %s", host)
			continue
		}
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("v5 video_model http %d from %s", resp.StatusCode, host)
			continue
		}
		var root struct {
			Code    int             `json:"code"`
			CapCode int             `json:"Code"`
			Msg     string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &root); err != nil {
			lastErr = err
			continue
		}
		code := root.Code
		if code == 0 {
			code = root.CapCode
		}
		if code != 0 {
			if rej := identityRejection(code, root.Msg); rej != nil {
				return nil, rej
			}
			lastErr = fmt.Errorf("v5 video_model code %d: %s", code, root.Msg)
			continue
		}
		var vmBytes []byte
		var direct struct {
			VideoModel json.RawMessage `json:"video_model"`
		}
		if err := json.Unmarshal(root.Data, &direct); err == nil && len(direct.VideoModel) > 0 {
			vmBytes = direct.VideoModel
			if vmBytes[0] == '"' {
				var s string
				if json.Unmarshal(vmBytes, &s) == nil {
					vmBytes = []byte(s)
				}
			}
		} else {
			vmBytes = root.Data
		}
		st, perr := pickStreamFromVideoModel(vmBytes, definition)
		if perr != nil {
			lastErr = perr
			continue
		}
		return st, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("v5 video_model failed")
	}
	return nil, lastErr
}

// videoTabMultiVideoModel 使用 v5 签名请求 /reading/bookapi/video_tab/multi_video_model/v1/。
func (c *Client) videoTabMultiVideoModel(dev *RegisteredDevice, signer *AppSigner, itemID, definition string) (*AppStream, error) {
	if signer == nil {
		signer = NewAppSigner()
	}
	query := v5PlaybackQuery{
		DeviceID:    dev.DeviceID,
		InstallID:   dev.InstallID,
		CDID:        dev.CDID,
		VersionName: v5PreloadVersionName,
		Channel:     v5PreloadChannel,
		DeviceType:  v5PreloadDeviceType,
		OSVersion:   v5PreloadOSVersion,
	}
	extra := [][2]string{
		{"video_id", itemID},
		{"video_id_type", "0"},
		{"device_level", "1"},
		{"need_all_video_definition", "1"},
		{"need_mp4_align", "0"},
		{"use_os_player", "0"},
		{"video_platform", "1024"},
		{"source", "0"},
		{"from_video_id", ""},
	}
	hosts := []string{
		"api5-normal-sinfonlineb.fqnovel.com",
		"api5-normal-sinfonlinea.fqnovel.com",
		"api5-normal-hl.fqnovel.com",
		"api5-normal-lf.fqnovel.com",
	}
	anonClient := &http.Client{Timeout: c.HTTP.Timeout, Transport: c.HTTP.Transport}
	var lastErr error
	for _, host := range hosts {
		baseURL := "https://" + host + "/reading/bookapi/video_tab/multi_video_model/v1/"
		fullURL, headers, err := signer.SignPlaybackV5WithExtra(baseURL, query, extra, nil, currentMillis())
		if err != nil {
			lastErr = err
			continue
		}
		req, err := http.NewRequest(http.MethodGet, fullURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Accept", "application/json")
		resp, err := anonClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			continue
		}
		if len(raw) == 0 {
			lastErr = fmt.Errorf("videoTab empty response from %s (status %d)", host, resp.StatusCode)
			continue
		}
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("videoTab http %d from %s", resp.StatusCode, host)
			continue
		}
		st, perr := parseAppVideoModel(raw, itemID, definition)
		if perr != nil {
			if upstream.IsIdentityRejected(perr) {
				return nil, perr
			}
			lastErr = perr
			continue
		}
		return st, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("videoTab failed")
	}
	return nil, lastErr
}

func (c *Client) singleVideoModel(dev *RegisteredDevice, itemID, wantDef string) (*AppStream, error) {
	payload, _ := json.Marshal(map[string]any{
		"biz_param": map[string]any{
			"need_all_video_definition": true,
			"video_platform":            3,
		},
		"content_type": 1,
		"video_id":     itemID,
	})
	cli := &http.Client{Timeout: c.HTTP.Timeout, Transport: c.HTTP.Transport}
	var lastErr error
	for _, host := range appPlayHosts {
		nowMs := currentMillis()
		query := sortedDeviceQuery(dev, nowMs)
		u := "https://" + host + "/novel/player/video_model/v1/?" + query
		req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", strings.ReplaceAll(appUA, "/72232", "/73532"))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("Sdk-Version", "2")
		req.Header.Set("X-XS-From-Web", "0")
		for k, v := range SingleVideoHeaders(query, payload, dev.DeviceID, nowMs) {
			req.Header.Set(k, v)
		}
		resp, err := cli.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if len(raw) == 0 {
			lastErr = fmt.Errorf("singleVideoModel empty response from %s (status %d)", host, resp.StatusCode)
			continue
		}
		var root struct {
			Code    int             `json:"code"`
			CapCode int             `json:"Code"`
			Msg     string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &root); err != nil {
			lastErr = fmt.Errorf("singleVideoModel json: %w", err)
			continue
		}
		code := root.Code
		if code == 0 {
			code = root.CapCode
		}
		if code != 0 {
			if rej := identityRejection(code, root.Msg); rej != nil {
				// 设备身份被拒：换 host 也不会变好，直接把身份问题抛出去。
				return nil, rej
			}
			lastErr = fmt.Errorf("singleVideoModel code %d: %s", code, root.Msg)
			continue
		}
		var dataObj struct {
			VideoModel json.RawMessage `json:"video_model"`
		}
		if err := json.Unmarshal(root.Data, &dataObj); err != nil {
			lastErr = err
			continue
		}
		vmBytes := dataObj.VideoModel
		if len(vmBytes) == 0 {
			vmBytes = root.Data
		}
		if len(vmBytes) > 0 && vmBytes[0] == '"' {
			var s string
			if err := json.Unmarshal(vmBytes, &s); err == nil {
				if dec, err := base64.StdEncoding.DecodeString(s); err == nil && len(dec) > 0 && dec[0] == '{' {
					vmBytes = dec
				} else {
					vmBytes = []byte(s)
				}
			}
		}
		st, err := pickStreamFromVideoModel(vmBytes, wantDef)
		if err != nil {
			lastErr = err
			continue
		}
		return st, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("singleVideoModel failed")
	}
	return nil, lastErr
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func parseAppVideoModel(raw []byte, itemID, wantDef string) (*AppStream, error) {
	var root struct {
		Code    int             `json:"code"`
		CapCode int             `json:"Code"`
		Msg     string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("app video_model json: %w", err)
	}
	code := root.Code
	if code == 0 {
		code = root.CapCode
	}
	if code != 0 {
		if rej := identityRejection(code, root.Msg); rej != nil {
			return nil, rej
		}
		return nil, fmt.Errorf("app video_model code %d: %s", code, root.Msg)
	}
	var dataMap map[string]json.RawMessage
	if err := json.Unmarshal(root.Data, &dataMap); err != nil {
		return nil, fmt.Errorf("app data map: %w", err)
	}
	rowRaw := locateVideoModelRow(dataMap, itemID)
	if len(rowRaw) == 0 {
		return nil, fmt.Errorf("app video_model empty for %s", itemID)
	}
	var row struct {
		VideoModel json.RawMessage `json:"video_model"`
	}
	if err := json.Unmarshal(rowRaw, &row); err != nil {
		return nil, err
	}
	vmBytes := row.VideoModel
	if len(vmBytes) > 0 && vmBytes[0] == '"' {
		var s string
		if err := json.Unmarshal(vmBytes, &s); err == nil {
			vmBytes = []byte(s)
		}
	}
	return pickStreamFromVideoModel(vmBytes, wantDef)
}

// locateVideoModelRow 从响应数据里定位目标剧集的播放模型条目。
//
// 上游有两种形态：
//   - data 直接按剧集 id 分组：data[episodeID]
//   - data 先按业务类型分组：data["1004"][episodeID]
//
// 只认精确 id。此前"取任意一条"的回退会让批量响应里播错集 —— 上游
// PlaybackModels.model 明确禁止这种行为（"Never silently play a different
// episode from a batch response"）。v3 单视频接口有自己的扁平兜底，不在这里。
func locateVideoModelRow(dataMap map[string]json.RawMessage, itemID string) json.RawMessage {
	if row, ok := dataMap[itemID]; ok && len(row) > 0 {
		return row
	}
	for _, groupKey := range []string{"1004", "video_model_data"} {
		if group, ok := dataMap[groupKey]; ok && len(group) > 0 {
			var inner map[string]json.RawMessage
			if err := json.Unmarshal(group, &inner); err == nil {
				if row, ok := inner[itemID]; ok && len(row) > 0 {
					return row
				}
			}
		}
	}
	return nil
}

// appVideoRendition 是一个档位的线格式。上游把编码标在三个不同位置，
// URL 也有四个候选字段 —— 只读某一个会在上游微调字段时整集解析失败。
type appVideoRendition struct {
	MainURL     string `json:"main_url"`
	PlayURL     string `json:"play_url"`
	BackupURL   string `json:"backup_url"`
	BackupURL1  string `json:"backup_url_1"`
	CodecType   string `json:"codec_type"`
	GearDesKey  string `json:"gear_des_key"`
	SpadeA      string `json:"spade_a"`
	EncryptInfo struct {
		SpadeA  string `json:"spade_a"`
		Encrypt any    `json:"encrypt"`
	} `json:"encrypt_info"`
	VideoMeta struct {
		Definition string `json:"definition"`
		CodecType  string `json:"codec_type"`
		VWidth     int    `json:"vwidth"`
		VHeight    int    `json:"vheight"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
	} `json:"video_meta"`
}

// decodeRenditions 兼容 video_list 的两种线格式：数组，或以清晰度名为 key 的对象。
func decodeRenditions(raw json.RawMessage) []appVideoRendition {
	if len(raw) == 0 {
		return nil
	}
	var rows []appVideoRendition
	if err := json.Unmarshal(raw, &rows); err == nil {
		return rows
	}
	var byName map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byName); err != nil {
		return nil
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var row appVideoRendition
		if err := json.Unmarshal(byName[name], &row); err == nil {
			rows = append(rows, row)
		}
	}
	return rows
}

// isExcludedCodec 复刻上游 PlaybackModels.renditions 的 HEVC 私有档排除规则：
// bytevc2 按子串排除，bvc2 必须作为独立 token 出现（避免误伤 xbvc2x 之类）。
func isExcludedCodec(codec string) bool {
	if strings.Contains(codec, "bytevc2") {
		return true
	}
	for _, token := range strings.FieldsFunc(codec, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if token == "bvc2" {
			return true
		}
	}
	return false
}

// isFalsyEncrypt 判断 encrypt_info.encrypt 是否明确表示"未加密"。
// 缺省（nil）按未加密处理；true / 1 / 非空串都视为需要 key。
func isFalsyEncrypt(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case bool:
		return !x
	case float64:
		return x == 0
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "" || s == "0" || s == "false"
	default:
		return false
	}
}

func pickStreamFromVideoModel(vmBytes []byte, wantDef string) (*AppStream, error) {
	return pickStreamFromVideoModelWithFallback(vmBytes, wantDef, true)
}

func pickStreamFromVideoModelNoFallback(vmBytes []byte, wantDef string) (*AppStream, error) {
	return pickStreamFromVideoModelWithFallback(vmBytes, wantDef, false)
}

func pickStreamFromVideoModelWithFallback(vmBytes []byte, wantDef string, allowFallback bool) (*AppStream, error) {
	var vm struct {
		KeySeed   string          `json:"key_seed"`
		VideoList json.RawMessage `json:"video_list"`
	}
	if err := json.Unmarshal(vmBytes, &vm); err != nil {
		return nil, fmt.Errorf("parse video_model: %w", err)
	}
	var seed []byte
	if vm.KeySeed != "" {
		seed, _ = base64.StdEncoding.DecodeString(vm.KeySeed)
	}

	var best *AppStream
	bestScore := -1
	for _, item := range decodeRenditions(vm.VideoList) {
		// 编码判据合并三处，并以最低层的 video_meta.codec_type 作为对外编码名，
		// 保持与既有探针输出一致（bytevc1）。
		codecHint := strings.ToLower(strings.Join([]string{
			item.CodecType, item.GearDesKey, item.VideoMeta.CodecType,
		}, " "))
		if isExcludedCodec(codecHint) {
			continue
		}
		codec := strings.ToLower(item.VideoMeta.CodecType)

		main := strings.TrimSpace(item.MainURL)
		play := strings.TrimSpace(item.PlayURL)
		bak := strings.TrimSpace(item.BackupURL)
		bak1 := strings.TrimSpace(item.BackupURL1)
		u := firstNonEmpty(main, play, bak, bak1)
		if u == "" {
			continue
		}
		bu := firstNonEmpty(bak, bak1, play, main)
		if bu == u {
			bu = ""
		}

		resolveURL := func(raw string) (string, bool) {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				return "", false
			}
			if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
				return raw, true
			}
			if len(seed) == 0 {
				return "", false
			}
			if dec, err := decryptSpadeURL(raw, seed); err == nil {
				return dec, true
			}
			if dec, err := base64.StdEncoding.DecodeString(raw); err == nil && len(dec) > 0 {
				return string(dec), true
			}
			return "", false
		}
		u, ok := resolveURL(u)
		if !ok {
			continue
		}
		if bu != "" {
			if decoded, ok := resolveURL(bu); ok {
				bu = decoded
			} else {
				bu = ""
			}
		}

		spade := firstNonEmpty(item.EncryptInfo.SpadeA, item.SpadeA)
		var key []byte
		if spade != "" {
			k, err := DeriveSpadeKey(spade)
			if err != nil {
				continue
			}
			key = k
		} else if !isFalsyEncrypt(item.EncryptInfo.Encrypt) {
			// 明确声明加密却拿不到 key：这档解不出来，换下一档。
			continue
		}

		w := item.VideoMeta.VWidth
		if w == 0 {
			w = item.VideoMeta.Width
		}
		h := item.VideoMeta.VHeight
		if h == 0 {
			h = item.VideoMeta.Height
		}
		qNum := parseQualityNum(item.VideoMeta.Definition)
		score := qNum
		if wantDef != "" && strings.EqualFold(item.VideoMeta.Definition, wantDef) {
			score += 10000
		}
		// bytevc1 实为 HEVC（实测 moov 里是 hvcC/hvc1）。只有真正的 H.264
		// 才加分，这样 WebView 能硬解的档位始终优先。
		if strings.Contains(codecHint, "h264") || strings.Contains(codecHint, "avc1") || strings.Contains(codecHint, "avc3") {
			score += 50000
		}
		if score > bestScore {
			bestScore = score
			best = &AppStream{
				Definition: item.VideoMeta.Definition,
				Codec:      codec,
				Width:      w,
				Height:     h,
				URL:        u,
				BackupURL:  bu,
				Key:        key,
			}
		}
	}
	if best == nil {
		if allowFallback {
			var fb struct {
				FallbackAPI string `json:"fallback_api"`
			}
			if json.Unmarshal(vmBytes, &fb) == nil && strings.HasPrefix(fb.FallbackAPI, "https://") {
				if st, ferr := fetchCompatibleFallback(fb.FallbackAPI, seed, wantDef, []string{"1", "3"}); ferr == nil {
					return st, nil
				}
			}
		}
		return nil, fmt.Errorf("no playable video in app video_model")
	}
	// 主模型只有 HEVC(bytevc1) 而无 H.264 时，若带 fallback_api，主动尝试协商
	// H.264 (codec_type=1) 兼容档位，规避部分设备 WebView 硬解 HEVC 几秒后无声/卡帧的问题。
	if allowFallback && !isH264Codec(best.Codec) {
		var fb struct {
			FallbackAPI string `json:"fallback_api"`
		}
		if json.Unmarshal(vmBytes, &fb) == nil && strings.HasPrefix(fb.FallbackAPI, "https://") {
			if st, ferr := fetchCompatibleFallback(fb.FallbackAPI, seed, wantDef, []string{"1"}); ferr == nil && isH264Codec(st.Codec) {
				return st, nil
			}
		}
	}
	return best, nil
}

func isH264Codec(codec string) bool {
	c := strings.ToLower(strings.TrimSpace(codec))
	return strings.Contains(c, "h264") || strings.Contains(c, "avc1") || strings.Contains(c, "avc3")
}

// fetchCompatibleFallback mirrors hongguoTV's compatibleLadder: query
// fallback_api with requested codec_type values (e.g. 1=H.264, 3=HEVC).
func fetchCompatibleFallback(endpoint string, seed []byte, wantDef string, codecs []string) (*AppStream, error) {
	cli := &http.Client{Timeout: 6 * time.Second}
	for _, codec := range codecs {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("codec_type", codec)
		u.RawQuery = q.Encode()

		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			continue
		}
		req.Header.Set("Accept", "application/json")
		resp, err := cli.Do(req)
		if err != nil {
			continue
		}
		raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if rerr != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		var outer struct {
			VideoInfo struct {
				Data json.RawMessage `json:"data"`
			} `json:"video_info"`
		}
		if json.Unmarshal(raw, &outer) != nil || len(outer.VideoInfo.Data) == 0 {
			continue
		}
		if st, perr := pickStreamFromVideoModelNoFallback(outer.VideoInfo.Data, wantDef); perr == nil {
			return st, nil
		}
	}
	return nil, fmt.Errorf("no compatible fallback stream")
}

func parseQualityNum(def string) int {
	s := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(def)), "p")
	n, _ := strconv.Atoi(s)
	return n
}
