package hongguo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/xiangkunzhang/EkkoVideo/internal/provider/online/upstream"
)

// v5 播放接口的设备声明。
//
// 这些值描述的是"请求方把自己当成什么设备"，与注册时用的设备参数是两回事：
// 协议允许注册身份与声明身份不一致（上游就是注册平板身份、声明电视身份）。
const (
	v5VersionName = "7.4.3.32"
	v5Channel     = "vivo_8662_64"
	v5DeviceType  = "PGT-AN10"
	v5OSVersion   = "12"

	v5PreloadVersionName = "7.4.5.32"
	v5PreloadChannel     = "update_64"
	v5PreloadDeviceType  = "25053RT47C"
	v5PreloadOSVersion   = "16"
)

func marshalV5PlaybackBody(itemID string, deviceLevel int, drScene string) ([]byte, error) {
	payload, err := json.Marshal(map[string]any{
		"biz_param": map[string]any{
			"detail_page_version":       0,
			"device_level":              deviceLevel,
			"disable_digg_stat":         false,
			"disable_video_relate_book": false,
			"need_all_video_definition": true,
			"need_mp4_align":            false,
			"use_os_player":             false,
			"use_server_dns":            false,
			"video_platform":            1024,
		},
		"mixed_video_id_map": map[string]any{
			"1004": []string{itemID},
		},
		"dr_scene": drScene,
	})
	if err != nil {
		return nil, fmt.Errorf("hongguo: v5 payload: %w", err)
	}
	body, err := gzipBytes(payload)
	if err != nil {
		return nil, fmt.Errorf("hongguo: v5 gzip: %w", err)
	}
	return body, nil
}

// multiVideoModelV5 用 v5 签名请求播放模型。
//
// 与旧版签名的三点关键差异：
//   - 请求体先 gzip 再签名，x-ss-stub 也按 gzip 后的字节算
//   - 只携带 x-khronos / x-helios / x-medusa 三个签名头，且不带任何 Cookie
//   - 优先走 preload/v1（7.4.5.32 / dr_scene=preload），再回退 player/v1（7.4.3.32 / dr_scene=player）
func (c *Client) multiVideoModelV5(dev *RegisteredDevice, signer *AppSigner, itemID, definition string) (*AppStream, error) {
	// 对齐 hongguoTV 0.16.17：preload/v1 使用 device_level=4 直接协商兼容编码档位，减少额外 VOD 协商。
	preloadBody, err := marshalV5PlaybackBody(itemID, 4, "preload")
	if err != nil {
		return nil, err
	}
	playerBody, err := marshalV5PlaybackBody(itemID, 1, "player")
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

	type v5Target struct {
		host    string
		baseURL string
		query   v5PlaybackQuery
		body    []byte
	}
	targets := []v5Target{
		{
			host:    "reading.snssdk.com",
			baseURL: "https://reading.snssdk.com/novel/player/multi_video_model/preload/v1",
			query: v5PlaybackQuery{
				DeviceID:    dev.DeviceID,
				InstallID:   dev.InstallID,
				CDID:        dev.CDID,
				VersionName: v5PreloadVersionName,
				Channel:     v5PreloadChannel,
				DeviceType:  v5PreloadDeviceType,
				OSVersion:   v5PreloadOSVersion,
			},
			body: preloadBody,
		},
	}
	hosts := appPlayHosts
	if c.Routes != nil {
		if planned := c.Routes.Plan(false); len(planned) > 0 {
			hosts = planned
		}
	}
	for _, host := range hosts {
		targets = append(targets, v5Target{
			host:    host,
			baseURL: "https://" + host + "/novel/player/multi_video_model/player/v1",
			query:   query,
			body:    playerBody,
		})
	}

	anonClient := &http.Client{Timeout: c.HTTP.Timeout, Transport: c.HTTP.Transport}
	var lastErr error
	for _, tgt := range targets {
		host := tgt.host
		fullURL, headers, err := signer.SignPlaybackV5(tgt.baseURL, tgt.query, tgt.body, currentMillis())
		if err != nil {
			lastErr = err
			continue
		}
		req, err := http.NewRequest(http.MethodPost, fullURL, bytes.NewReader(tgt.body))
		if err != nil {
			lastErr = err
			continue
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if tgt.query.VersionName == v5PreloadVersionName {
			req.Header.Set("User-Agent", "okhttp/4.12.0")
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Encoding", "gzip")

		resp, err := anonClient.Do(req)
		if err != nil {
			if c.Routes != nil {
				c.Routes.OnFailure(host, true, false)
			}
			lastErr = err
			continue
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			if c.Routes != nil {
				c.Routes.OnFailure(host, true, false)
			}
			lastErr = err
			continue
		}
		if len(raw) == 0 {
			// 上游对签名不被识别的请求返回 200 空包，这里不是节点故障。
			if c.Routes != nil {
				c.Routes.OnFailure(host, false, true)
			}
			lastErr = fmt.Errorf("hongguo: v5 empty response from %s", host)
			continue
		}
		if resp.StatusCode >= 400 {
			if c.Routes != nil {
				c.Routes.OnFailure(host, resp.StatusCode < 500, false)
			}
			lastErr = fmt.Errorf("hongguo: v5 http %d from %s", resp.StatusCode, host)
			continue
		}
		st, err := parseAppVideoModel(raw, itemID, definition)
		if err != nil {
			if upstream.IsIdentityRejected(err) {
				// 身份被拒与节点无关：换 host 没有意义，立刻返回。
				return nil, err
			}
			if c.Routes != nil {
				c.Routes.OnSuccess(host, true) // host 是健康的，只是没给我们要的数据
			}
			lastErr = err
			continue
		}
		if c.Routes != nil {
			c.Routes.OnSuccess(host, false)
		}
		return st, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("hongguo: v5 resolve failed")
	}
	return nil, lastErr
}
