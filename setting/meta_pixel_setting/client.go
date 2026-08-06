package meta_pixel_setting

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const metaPixelGraphAPI = "https://graph.facebook.com"
const metaPixelGraphVersion = "v21.0"

type metaPixelEvent struct {
	EventID      string         `json:"event_id,omitempty"`
	EventName    string         `json:"event_name"`
	EventTime    int64          `json:"event_time"`
	ActionSource string         `json:"action_source"`
	UserData     map[string]any `json:"user_data,omitempty"`
	CustomData   map[string]any `json:"custom_data,omitempty"`
}

type metaPixelRequest struct {
	Data []metaPixelEvent `json:"data"`
}

// reportEvent 异步上报单个事件。失败仅记日志,不阻塞业务。
func reportEvent(ev metaPixelEvent) {
	if !IsEnabled() {
		return
	}
	go func() {
		cfg := GetMetaPixelSetting()
		payload, err := common.Marshal(metaPixelRequest{Data: []metaPixelEvent{ev}})
		if err != nil {
			common.SysError("meta_pixel: marshal event failed: " + err.Error())
			return
		}
		url := fmt.Sprintf("%s/%s/%s/events?access_token=%s",
			metaPixelGraphAPI, metaPixelGraphVersion, cfg.PixelID, cfg.AccessToken)
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			common.SysError("meta_pixel: build request failed: " + err.Error())
			return
		}
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			common.SysError("meta_pixel: send event failed: " + err.Error())
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if resp.StatusCode >= 300 {
			common.SysError(fmt.Sprintf("meta_pixel: send event %s failed status=%s body=%s",
				ev.EventName, resp.Status, string(body)))
		}
	}()
}

// TrackCompleteRegistration 注册完成事件。邮箱 SHA-256 哈希用于高级匹配,
// 用户 ID 作为 external_id;邮箱为空时仅携带 external_id。
func TrackCompleteRegistration(userId int, email string) {
	if !IsEnabled() {
		return
	}
	userData := map[string]any{
		"external_id": []string{strconv.Itoa(userId)},
	}
	if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
		userData["em"] = []string{hex.EncodeToString(common.Sha256Raw([]byte(email)))}
	}
	reportEvent(metaPixelEvent{
		EventID:      fmt.Sprintf("CompleteRegistration_%d", userId),
		EventName:    "CompleteRegistration",
		EventTime:    common.GetTimestamp(),
		ActionSource: "website",
		UserData:     userData,
	})
}

// TrackPurchase 充值到账事件。event_id 用订单号,便于与浏览器端事件同源去重。
func TrackPurchase(tradeNo string, value float64, userId int) {
	if !IsEnabled() || tradeNo == "" {
		return
	}
	reportEvent(metaPixelEvent{
		EventID:      tradeNo,
		EventName:    "Purchase",
		EventTime:    common.GetTimestamp(),
		ActionSource: "website",
		UserData: map[string]any{
			"external_id": []string{strconv.Itoa(userId)},
		},
		CustomData: map[string]any{
			"value":    value,
			"currency": "USD",
		},
	})
}
