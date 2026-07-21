package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type LegalSettings struct {
	UserAgreement string `json:"user_agreement"`
	PrivacyPolicy string `json:"privacy_policy"`
	DeliveryTerms string `json:"delivery_terms"`
	RefundPolicy  string `json:"refund_policy"`
	ContactUs     string `json:"contact_us"`
}

var defaultLegalSettings = LegalSettings{
	UserAgreement: "",
	PrivacyPolicy: "",
	DeliveryTerms: "",
	RefundPolicy:  "",
	ContactUs:     "",
}

func init() {
	config.GlobalConfig.Register("legal", &defaultLegalSettings)
}

func GetLegalSettings() *LegalSettings {
	return &defaultLegalSettings
}
