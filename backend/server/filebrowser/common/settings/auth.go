package settings

type Auth struct {
	TokenExpirationHours int    `json:"tokenExpirationHours"` // time in hours each web UI session token is valid for. Default is 2 hours.
	Key                  string `json:"key"`                  // secret: the key used to sign the JWT tokens. If not set, a random key will be generated.
}
