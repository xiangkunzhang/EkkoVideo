package hongguo

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	deviceRegisterURL    = "https://log5-applog-lf.fqnovel.com/service/2/device_register/?tt_data=a&cronet_version=8d40f833_2026-03-03&ttnet_version=4.2.243.31-douyin&use_store_region_cookie=1"
	deviceRegisterAltURL = "https://is.snssdk.com/service/2/device_register/?tt_data=a&cronet_version=8d40f833_2026-03-03&ttnet_version=4.2.243.31-douyin&use_store_region_cookie=1"
	appUA                = "com.phoenix.read/72232 (Linux; U; Android 16; zh_CN; 25053RT47C; Build/BP2A.250605.031.A3)"
	playerUA             = "com.phoenix.read/71532 (Linux; U; Android 9; SM-N9860; Build/PQ3A.190705.10241111;tt-ok/3.12.13.20)"
)

var (
	ttEncryptMagic = []byte{0x74, 0x63, 0x05, 0x10, 0x00, 0x00}
	ttKDFSaltHex   = "4dd4c2e6b83162090e52b3c7a6733ba41cb2462b829ab58a196b39db57177524f49baf7f08e8d68d26a72e37c1a95a2f1f05a51892aef2949732b62a38aadd58"
)

type RegisteredDevice struct {
	DeviceID     string
	InstallID    string
	CDID         string
	DeviceToken  string
	CookieHeader string
}

func newUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func ttEncrypt(plain []byte) ([]byte, error) {
	rnd := make([]byte, 32)
	if _, err := rand.Read(rnd); err != nil {
		return nil, err
	}
	salt, _ := hex.DecodeString(ttKDFSaltHex)
	h1 := sha512.Sum512(rnd)
	h2Input := append(h1[:], salt...)
	derived := sha512.Sum512(h2Input)

	hData := sha512.Sum512(plain)
	body := make([]byte, 0, len(hData)+len(plain)+16)
	body = append(body, hData[:]...)
	body = append(body, plain...)
	pad := 16 - (len(body) % 16)
	for i := 0; i < pad; i++ {
		body = append(body, byte(pad))
	}

	blk, err := aes.NewCipher(derived[:16])
	if err != nil {
		return nil, err
	}
	enc := make([]byte, len(body))
	cipher.NewCBCEncrypter(blk, derived[16:32]).CryptBlocks(enc, body)

	out := make([]byte, 0, len(ttEncryptMagic)+len(rnd)+len(enc))
	out = append(out, ttEncryptMagic...)
	out = append(out, rnd...)
	out = append(out, enc...)
	return out, nil
}

func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RegisterDevice performs anonymous device registration against fqnovel.com without cookies.
func RegisterDevice(httpClient *http.Client) (*RegisteredDevice, error) {
	// Never send account/catalog cookies during device registration; fqnovel rejects with device_id=0.
	var tr http.RoundTripper
	if httpClient != nil {
		tr = httpClient.Transport
	}
	anonClient := &http.Client{Timeout: 20 * time.Second, Transport: tr}

	var lastErr error
	for _, endpoint := range []string{deviceRegisterURL, deviceRegisterAltURL} {
		dev, err := registerDeviceOnce(anonClient, endpoint)
		if err == nil {
			return dev, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// DeviceConfigDir overrides the directory where app_device.json is stored.
var DeviceConfigDir string

func appDeviceCachePath() string {
	if DeviceConfigDir != "" {
		return filepath.Join(DeviceConfigDir, "app_device.json")
	}
	return filepath.Join("data", "config", "app_device.json")
}

func loadCachedDevice() *RegisteredDevice {
	raw, err := os.ReadFile(appDeviceCachePath())
	if err != nil {
		return nil
	}
	var dev RegisteredDevice
	if err := json.Unmarshal(raw, &dev); err != nil {
		return nil
	}
	if dev.DeviceID == "" || dev.DeviceID == "0" || dev.InstallID == "" || dev.InstallID == "0" {
		return nil
	}
	return &dev
}

func saveCachedDevice(dev *RegisteredDevice) {
	if dev == nil {
		return
	}
	p := appDeviceCachePath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if raw, err := json.Marshal(dev); err == nil {
		_ = os.WriteFile(p, raw, 0o600)
	}
}

func registerDeviceOnce(httpClient *http.Client, endpoint string) (*RegisteredDevice, error) {
	now := time.Now().UnixMilli()
	cdid := newUUIDv4()
	openudid := strings.ReplaceAll(newUUIDv4(), "-", "")[:16]

	payloadMap := map[string]any{
		"magic_tag": "ss_app_log",
		"_gen_time": now,
		"header": map[string]any{
			"display_name":          "红果免费短剧",
			"update_version_code":   73932,
			"manifest_version_code": 73932,
			"app_version_minor":     "7.3.9.32",
			"aid":                   8662,
			"channel":               "update_64",
			"package":               "com.phoenix.read",
			"app_version":           "7.3.9.32",
			"version_code":          73932,
			"sdk_version":           "3.7.0-rc.34-fanqie-xiaoshuo",
			"sdk_target_version":    29,
			"git_hash":              "3a88f4e",
			"os":                    "Android",
			"os_version":            "16",
			"os_api":                36,
			"device_model":          "25053RT47C",
			"device_brand":          "Redmi",
			"device_manufacturer":   "Redmi",
			"cpu_abi":               "arm64-v8a",
			"release_build":         "b5e187f",
			"density_dpi":           520,
			"display_density":       "mdpi",
			"resolution":            "1280x2772",
			"language":              "zh",
			"timezone":              8,
			"access":                "wifi",
			"not_request_sender":    0,
			"carrier":               "CHINA MOBILE",
			"mcc_mnc":               "46000",
			"rom":                   "eng.root.20231116.160340",
			"rom_version":           "W528JS release-keys",
			"cdid":                  cdid,
			"sig_hash":              "56a962410c494bbaf0b58dba20cae56f",
			"openudid":              openudid,
			"clientudid":            newUUIDv4(),
			"ipv6_list":             []any{},
			"region":                "CN",
			"tz_name":               "Asia/Shanghai",
			"tz_offset":             28800,
			"sim_region":            "cn",
			"sim_serial_number":     []any{},
			"oaid_may_support":      false,
			"req_id":                newUUIDv4(),
			"device_platform":       "android",
			"custom": map[string]any{
				"account_region_src": "fallback",
				"host_bit":           64,
				"account_region":     "cn",
				"dragon_device_type": "phone",
			},
			"pre_installed_channel":  "",
			"apk_first_install_time": now,
			"is_system_app":          0,
			"sdk_flavor":             "china",
			"guest_mode":             0,
		},
	}

	rawJSON, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, err
	}
	gz, err := gzipBytes(rawJSON)
	if err != nil {
		return nil, err
	}
	body, err := ttEncrypt(gz)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	stub := md5.Sum(body)
	req.Header.Set("User-Agent", appUA)
	req.Header.Set("Content-Type", "application/octet-stream;tt-data=a")
	req.Header.Set("log-encode-type", "gzip")
	req.Header.Set("x-ss-dp", "8662")
	req.Header.Set("x-ss-req-ticket", fmt.Sprintf("%d", now))
	req.Header.Set("x-vc-bdturing-sdk-version", "4.0.3.cn")
	req.Header.Set("lc", "101")
	req.Header.Set("sdk-version", "2")
	req.Header.Set("passport-sdk-version", "5051452")
	req.Header.Set("x-neptune", "-8|50:51:59:30:40:47:49:39")
	req.Header.Set("x-ss-stub", strings.ToUpper(hex.EncodeToString(stub[:])))

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("device_register http %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		DeviceID    json.Number `json:"device_id"`
		DeviceIDStr string      `json:"device_id_str"`
		InstallID   json.Number `json:"install_id"`
		InstallStr  string      `json:"install_id_str"`
		DeviceToken string      `json:"device_token"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("device_register json: %w (%s)", err, string(respBody))
	}
	did := strings.TrimSpace(parsed.DeviceIDStr)
	if did == "" {
		did = parsed.DeviceID.String()
	}
	iid := strings.TrimSpace(parsed.InstallStr)
	if iid == "" {
		iid = parsed.InstallID.String()
	}
	if did == "" || did == "0" || iid == "" || iid == "0" {
		return nil, fmt.Errorf("device_register rejected: %s", string(respBody))
	}

	keep := map[string]bool{
		"install_id":       true,
		"ttreq":            true,
		"store-region":     true,
		"store-region-src": true,
	}
	var ckParts []string
	for _, sc := range resp.Cookies() {
		if keep[sc.Name] && sc.Value != "" {
			ckParts = append(ckParts, sc.Name+"="+sc.Value)
		}
	}

	return &RegisteredDevice{
		DeviceID:     did,
		InstallID:    iid,
		CDID:         cdid,
		DeviceToken:  parsed.DeviceToken,
		CookieHeader: strings.Join(ckParts, "; "),
	}, nil
}
