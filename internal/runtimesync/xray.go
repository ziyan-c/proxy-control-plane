package runtimesync

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	xraycommand "github.com/xtls/xray-core/app/proxyman/command"
	xraystats "github.com/xtls/xray-core/app/stats/command"
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

func (c XrayClient) QueryTraffic(ctx context.Context, node domain.ProxyNode, reset bool) ([]domain.TrafficDelta, error) {
	client, closeConn, err := c.statsClient(node)
	if err != nil {
		return nil, err
	}
	defer closeConn()

	rpcCtx, cancel := c.rpcContext(ctx)
	defer cancel()

	response, err := client.QueryStats(rpcCtx, &xraystats.QueryStatsRequest{
		Pattern: "user>>>pcp-",
		Reset_:  reset,
	})
	if err != nil {
		return nil, err
	}

	byAccount := make(map[string]*domain.TrafficDelta)
	for _, stat := range response.GetStat() {
		accountID, direction, ok := parseManagedTrafficStat(stat.GetName())
		if !ok || stat.GetValue() <= 0 {
			continue
		}
		delta := byAccount[accountID]
		if delta == nil {
			delta = &domain.TrafficDelta{ProxyAccountID: accountID}
			byAccount[accountID] = delta
		}
		switch direction {
		case "uplink":
			delta.UploadBytes += stat.GetValue()
		case "downlink":
			delta.DownloadBytes += stat.GetValue()
		}
	}

	result := make([]domain.TrafficDelta, 0, len(byAccount))
	for _, delta := range byAccount {
		if delta.UploadBytes == 0 && delta.DownloadBytes == 0 {
			continue
		}
		result = append(result, *delta)
	}
	return result, nil
}

func (c XrayClient) handlerClient(node domain.ProxyNode) (xraycommand.HandlerServiceClient, func(), error) {
	conn, closeConn, err := c.grpcConn(node)
	if err != nil {
		return nil, nil, err
	}
	return xraycommand.NewHandlerServiceClient(conn), closeConn, nil
}

func (c XrayClient) statsClient(node domain.ProxyNode) (xraystats.StatsServiceClient, func(), error) {
	conn, closeConn, err := c.grpcConn(node)
	if err != nil {
		return nil, nil, err
	}
	return xraystats.NewStatsServiceClient(conn), closeConn, nil
}

func (c XrayClient) grpcConn(node domain.ProxyNode) (*grpc.ClientConn, func(), error) {
	if strings.TrimSpace(node.RuntimeAPIHost) == "" || node.RuntimeAPIPort == 0 {
		return nil, nil, fmt.Errorf("node %s has incomplete runtime API address", node.Name)
	}
	address := net.JoinHostPort(node.RuntimeAPIHost, strconv.Itoa(node.RuntimeAPIPort))
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	return conn, func() { _ = conn.Close() }, nil
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

func parseManagedTrafficStat(name string) (string, string, bool) {
	const prefix = "user>>>"
	const trafficMarker = ">>>traffic>>>"
	if !strings.HasPrefix(name, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(name, prefix)
	email, direction, ok := strings.Cut(rest, trafficMarker)
	if !ok {
		return "", "", false
	}
	accountID, ok := domain.ProxyAccountIDFromRuntimeEmail(email)
	if !ok {
		return "", "", false
	}
	if direction != "uplink" && direction != "downlink" {
		return "", "", false
	}
	return accountID, direction, true
}
