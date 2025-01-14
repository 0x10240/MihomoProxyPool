package proxypool

import (
	"github.com/metacubex/mihomo/component/auth"
	"github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/listener"
	authStore "github.com/metacubex/mihomo/listener/auth"
	"github.com/metacubex/mihomo/tunnel"
)

type CListener = constant.InboundListener

func getListenerByLocalPort(localPort int, proxyName string) (CListener, error) {
	proxy := map[string]any{
		"name":  proxyName,
		"port":  localPort,
		"proxy": proxyName,
		"type":  "mixed",
	}

	l, err := listener.ParseListener(proxy)
	if l != nil {
		return l, err
	}

	return l, nil
}

func setProxyAuthUser(user, password string) {
	users := []auth.AuthUser{{User: user, Pass: password}}
	authenticator := auth.NewAuthenticator(users)
	authStore.Default.SetAuthenticator(authenticator)
}

func startListen(listeners map[string]CListener, dropOld bool) {
	listener.PatchInboundListeners(listeners, tunnel.Tunnel, dropOld)
}
