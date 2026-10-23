package apischema

type NTPServersRequest struct {
	Servers []string `json:"servers"`
}

type EnabledRequest struct {
	Enabled string `json:"enabled"`
}

type ISOTimeRequest struct {
	ISOTime string `json:"isoTime"`
}

type TimezoneRequest struct {
	Timezone string `json:"timezone"`
}
