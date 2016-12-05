package softlayer

import (
	"strconv"
	"time"

	"github.com/softlayer/softlayer-go/session"
)

const (
	SoftlayerAPIEndpointPublicDefault  = "https://api.softlayer.com/rest/v3.1"
	SoftlayerAPIEndpointPrivateDefault = "https://api.service.softlayer.com/rest/v3.1"
)

func NewSoftlayerClientSession(apiEndpoint, username, password, trace string, timeout int) *session.Session {
	debug, err := strconv.ParseBool(trace)
	if err != nil {
		debug = false
	}
	session := session.New(username, password, apiEndpoint)
	session.Debug = debug
	session.Timeout = time.Duration(timeout) * time.Second
	return session
}
