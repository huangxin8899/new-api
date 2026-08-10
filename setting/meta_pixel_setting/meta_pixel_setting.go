package meta_pixel_setting

import "github.com/QuantumNous/new-api/setting/config"

// MetaPixelConfig 单个像素的 Conversions API 配置。
// 支持配置多个像素实现独立归因,name 用于标注地区/用途,便于管理。
type MetaPixelConfig struct {
	Name        string `json:"name,omitempty"`
	PixelID     string `json:"pixel_id"`
	AccessToken string `json:"access_token"`
}

// MetaPixelSetting 存放 Meta Pixel Conversions API 配置,经 options 表持久化。
// Pixels 为多像素列表;PixelID/AccessToken 为单像素时代的旧字段,
// 仅在 Pixels 为空时作为回退,保证旧配置无需迁移即可继续工作。
type MetaPixelSetting struct {
	PixelID     string            `json:"pixel_id"`
	AccessToken string            `json:"access_token"`
	Pixels      []MetaPixelConfig `json:"pixels"`
}

var metaPixelSetting = MetaPixelSetting{}

func init() {
	config.GlobalConfig.Register("meta_pixel_setting", &metaPixelSetting)
}

func GetMetaPixelSetting() *MetaPixelSetting {
	return &metaPixelSetting
}

// GetPixels 返回生效的像素列表:优先使用 Pixels 数组,否则回退到旧的单像素字段。
func (s *MetaPixelSetting) GetPixels() []MetaPixelConfig {
	if len(s.Pixels) > 0 {
		return s.Pixels
	}
	if s.PixelID != "" && s.AccessToken != "" {
		return []MetaPixelConfig{{PixelID: s.PixelID, AccessToken: s.AccessToken}}
	}
	return nil
}

// IsEnabled 返回是否已配置至少一个有效像素(ID 与令牌均非空)
func IsEnabled() bool {
	for _, p := range GetMetaPixelSetting().GetPixels() {
		if p.PixelID != "" && p.AccessToken != "" {
			return true
		}
	}
	return false
}
