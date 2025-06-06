package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	cmtpubsub "github.com/cometbft/cometbft/libs/pubsub"
	cmtquery "github.com/cometbft/cometbft/libs/pubsub/query"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
)

// Subscribe for events via WebSocket.
// More: https://docs.cometbft.com/v0.37/rpc/#/Websocket/subscribe
func Subscribe(ctx *rpctypes.Context, query string) (*ctypes.ResultSubscribe, error) {
	{
		addr := ctx.RemoteAddr()

		if env.Adapter.EventBus.NumClients() >= env.Config.MaxSubscriptionClients {
			return nil, fmt.Errorf("max_subscription_clients %d reached", env.Config.MaxSubscriptionClients)
		} else if env.Adapter.EventBus.NumClientSubscriptions(addr) >= env.Config.MaxSubscriptionsPerClient {
			return nil, fmt.Errorf("max_subscriptions_per_client %d reached", env.Config.MaxSubscriptionsPerClient)
		} else if len(query) > maxQueryLength {
			return nil, errors.New("maximum query length exceeded")
		}

		env.Logger.Info("Subscribe to query", "remote", addr, "query", query)

		q, err := cmtquery.New(query)
		if err != nil {
			return nil, fmt.Errorf("failed to parse query: %w", err)
		}

		subCtx, cancel := context.WithTimeout(ctx.Context(), SubscribeTimeout)
		defer cancel()

		sub, err := env.Adapter.EventBus.Subscribe(subCtx, addr, q, env.Config.SubscriptionBufferSize)
		if err != nil {
			return nil, err
		}

		closeIfSlow := env.Config.CloseOnSlowClient

		// Capture the current ID, since it can change in the future.
		subscriptionID := ctx.JSONReq.ID
		go func() {
			for {
				select {
				case msg := <-sub.Out():
					var (
						resultEvent = &ctypes.ResultEvent{Query: query, Data: msg.Data(), Events: msg.Events()}
						resp        = rpctypes.NewRPCSuccessResponse(subscriptionID, resultEvent)
					)
					writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					err := ctx.WSConn.WriteRPCResponse(writeCtx, resp)
					cancel()
					if err != nil {
						env.Logger.Info("Can't write response (slow client)",
							"to", addr, "subscriptionID", subscriptionID, "err", err)

						if closeIfSlow {
							var (
								err  = errors.New("subscription was canceled (reason: slow client)")
								resp = rpctypes.RPCServerError(subscriptionID, err)
							)
							if !ctx.WSConn.TryWriteRPCResponse(resp) {
								env.Logger.Info("Can't write response (slow client)",
									"to", addr, "subscriptionID", subscriptionID, "err", err)
							}
							return
						}
					}
				case <-sub.Canceled():
					if !errors.Is(sub.Err(), cmtpubsub.ErrUnsubscribed) {
						var reason string
						if sub.Err() == nil {
							reason = "CometBFT exited"
						} else {
							reason = sub.Err().Error()
						}
						var (
							err  = fmt.Errorf("subscription was canceled (reason: %s)", reason)
							resp = rpctypes.RPCServerError(subscriptionID, err)
						)
						if !ctx.WSConn.TryWriteRPCResponse(resp) {
							env.Logger.Info("Can't write response (slow client)",
								"to", addr, "subscriptionID", subscriptionID, "err", err)
						}
					}
					return
				}
			}
		}()

		return &ctypes.ResultSubscribe{}, nil
	}
}

// Unsubscribe from events via WebSocket.
// More: https://docs.cometbft.com/v0.37/rpc/#/Websocket/unsubscribe
func Unsubscribe(ctx *rpctypes.Context, query string) (*ctypes.ResultUnsubscribe, error) {
	addr := ctx.RemoteAddr()
	env.Logger.Info("Unsubscribe from query", "remote", addr, "query", query)
	q, err := cmtquery.New(query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %w", err)
	}
	err = env.Adapter.EventBus.Unsubscribe(context.Background(), addr, q)
	if err != nil {
		return nil, err
	}
	return &ctypes.ResultUnsubscribe{}, nil

}

func UnsubscribeAll(ctx *rpctypes.Context) (*ctypes.ResultUnsubscribe, error) {
	addr := ctx.RemoteAddr()
	env.Logger.Info("Unsubscribe from all", "remote", addr)
	err := env.Adapter.EventBus.UnsubscribeAll(context.Background(), addr)
	if err != nil {
		return nil, err
	}
	return &ctypes.ResultUnsubscribe{}, nil
}
