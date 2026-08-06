package meta_pixel_setting

import "github.com/QuantumNous/new-api/setting/config"

// MetaPixelSetting 存放 Meta Pixel Conversions API 配置,经 options 表持久化。
type MetaPixelSetting struct {
	PixelID     string `json:"pixel_id"`
	AccessToken string `json:"access_token"`
}

var metaPixelSetting = MetaPixelSetting{}

func init() {
	config.GlobalConfig.Register("meta_pixel_setting", &metaPixelSetting)
}

func GetMetaPixelSetting() *MetaPixelSetting {
	return &metaPixelSetting
}

// IsEnabled 返回是否已配置(同时具备像素 ID 与访问令牌)
func IsEnabled() bool {
	return metaPixelSetting.PixelID != "" && metaPixelSetting.AccessToken != ""
}
