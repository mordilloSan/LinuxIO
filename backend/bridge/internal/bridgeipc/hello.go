package bridgeipc

// HelloRoute is the first call a bridge makes on a job host connection.
const HelloRoute = "jobs.hello"

// Hello introduces a bridge session to the job host.
type Hello struct {
	Protocol int          `json:"protocol"`
	Session  HelloSession `json:"session"`
}

// HelloSession is the bridge session a job host connection acts for.
type HelloSession struct {
	Username   string `json:"username"`
	UID        uint32 `json:"uid"`
	Privileged bool   `json:"privileged"`
	SessionRef string `json:"session_ref"`
}
