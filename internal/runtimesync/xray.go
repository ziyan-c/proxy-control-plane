package runtimesync

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	xraycommand "github.com/xtls/xray-core/app/proxyman/command"
	xrayprotocol "github.com/xtls/xray-core/common/protocol"
	xrayserial "github.com/xtls/xray-core/common/serial"
	xrayvless "github.com/xtls/xray-core/proxy/vless"
	"github.com/ziyan-c/proxy-control-plane/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type XrayClient struct {
	Timeout time.Duration
}

func (c XrayClient) ListUsers(ctx context.Context, node domain.ProxyNode) ([]domain.RuntimeUser, error) {
	client, closeConn, err := c.handlerClient(node)
	if err != nil {
		return nil, err
	}
	defer closeConn()

	rpcCtx, cancel := c.rpcContext(ctx)
	defer cancel()

	response, err := client.GetInboundUsers(rpcCtx, &xraycommand.GetInboundUserRequest{
		Tag: node.RuntimeInboundTag,
	})
	if err != nil {
		return nil, err
	}

	users := make([]domain.RuntimeUser, 0, len(response.GetUsers()))
	for _, user := range response.GetUsers() {
		runtimeUser, ok, err := parseVLESSUser(user)
		if err != nil {
			return nil, err
		}
		if ok {
			users = append(users, runtimeUser)
		}
	}
	return users, nil
}

func (c XrayClient) AddUser(ctx context.Context, node domain.ProxyNode, user domain.RuntimeUser) error {
	client, closeConn, err := c.handlerClient(node)
	if err != nil {
		return err
	}
	defer closeConn()

	rpcCtx, cancel := c.rpcContext(ctx)
	defer cancel()

	_, err = client.AlterInbound(rpcCtx, &xraycommand.AlterInboundRequest{
		Tag: node.RuntimeInboundTag,
		Operation: xrayserial.ToTypedMessage(&xraycommand.AddUserOperation{
			User: &xrayprotocol.User{
				Email: user.Email,
				Account: xrayserial.ToTypedMessage(&xrayvless.Account{
					Id:         user.UUID,
					Flow:       user.Flow,
					Encryption: xrayvless.None,
				}),
			},
		}),
	})
	return err
}

func (c XrayClient) RemoveUser(ctx context.Context, node domain.ProxyNode, email string) error {
	client, closeConn, err := c.handlerClient(node)
	if err != nil {
		return err
	}
	defer closeConn()

	rpcCtx, cancel := c.rpcContext(ctx)
	defer cancel()

	_, err = client.AlterInbound(rpcCtx, &xraycommand.AlterInboundRequest{
		Tag: node.RuntimeInboundTag,
		Operation: xrayserial.ToTypedMessage(&xraycommand.RemoveUserOperation{
			Email: email,
		}),
	})
	return err
}

func (c XrayClient) handlerClient(node domain.ProxyNode) (xraycommand.HandlerServiceClient, func(), error) {
	if strings.TrimSpace(node.RuntimeAPIHost) == "" || node.RuntimeAPIPort == 0 {
		return nil, nil, fmt.Errorf("node %s has incomplete runtime API address", node.Name)
	}
	address := net.JoinHostPort(node.RuntimeAPIHost, strconv.Itoa(node.RuntimeAPIPort))
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	return xraycommand.NewHandlerServiceClient(conn), func() { _ = conn.Close() }, nil
}

func (c XrayClient) rpcContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.Timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.Timeout)
}

func parseVLESSUser(user *xrayprotocol.User) (domain.RuntimeUser, bool, error) {
	if user == nil || user.GetAccount() == nil {
		return domain.RuntimeUser{}, false, nil
	}
	instance, err := user.GetAccount().GetInstance()
	if err != nil {
		return domain.RuntimeUser{}, false, err
	}
	account, ok := instance.(*xrayvless.Account)
	if !ok {
		return domain.RuntimeUser{}, false, nil
	}
	return domain.RuntimeUser{
		Email: user.GetEmail(),
		UUID:  account.GetId(),
		Flow:  account.GetFlow(),
	}, true, nil
}
