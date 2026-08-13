package nodeapi

import (
	"context"
	"net"
	"net/http"
	"testing"
)

func TestLocalControlRequestRejectsOtherHosts(t *testing.T) {
	request, _ := http.NewRequest(http.MethodPost, "http://node/control", nil)
	request.RemoteAddr = "10.0.0.2:1234"
	request = request.WithContext(context.WithValue(request.Context(), http.LocalAddrContextKey,
		&net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 8443}))
	if localControlRequest(request) {
		t.Fatal("different LAN host was treated as local")
	}
	request.RemoteAddr = "10.0.0.1:1234"
	if !localControlRequest(request) {
		t.Fatal("same-host interface address was rejected")
	}
	request.RemoteAddr = "127.0.0.1:1234"
	if !localControlRequest(request) {
		t.Fatal("loopback was rejected")
	}
}
