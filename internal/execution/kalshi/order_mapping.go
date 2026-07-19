package kalshi

import (
	"fmt"
	"math"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func mapCreateOrderRequest(order *domain.Order) (CreateOrderRequest, error) {
	if order == nil {
		return CreateOrderRequest{}, fmt.Errorf("kalshi: order is required")
	}

	ticker := strings.TrimSpace(order.Ticker)
	if ticker == "" {
		return CreateOrderRequest{}, fmt.Errorf("kalshi: ticker is required")
	}
	if order.Quantity <= 0 {
		return CreateOrderRequest{}, fmt.Errorf("kalshi: quantity must be positive")
	}
	count := int64(math.Round(order.Quantity))
	if math.Abs(order.Quantity-float64(count)) > 1e-9 || count <= 0 {
		return CreateOrderRequest{}, fmt.Errorf("kalshi: quantity must be a positive whole contract count")
	}
	contractSide, err := mapPredictionSide(order.PredictionSide)
	if err != nil {
		return CreateOrderRequest{}, err
	}

	req := CreateOrderRequest{
		Ticker:        ticker,
		Side:          contractSide,
		ClientOrderID: order.ID.String(),
		Count:         count,
	}

	switch order.Side {
	case domain.OrderSideBuy:
		req.Action = "buy"
	case domain.OrderSideSell:
		req.Action = "sell"
	default:
		return CreateOrderRequest{}, fmt.Errorf("kalshi: unsupported order side %q", order.Side)
	}

	switch order.OrderType {
	case domain.OrderTypeLimit:
		if order.LimitPrice == nil {
			return CreateOrderRequest{}, fmt.Errorf("kalshi: limit order requires limit price")
		}
		cents := int64(math.Round(*order.LimitPrice * 100))
		if cents <= 0 || cents >= 100 {
			return CreateOrderRequest{}, fmt.Errorf("kalshi: limit price must be between 0 and 1 exclusive")
		}
		req.Type = "limit"
		if contractSide == "yes" {
			req.YesPrice = &cents
		} else {
			req.NoPrice = &cents
		}
	case domain.OrderTypeMarket:
		req.Type = "market"
	default:
		return CreateOrderRequest{}, fmt.Errorf("kalshi: unsupported order type %q", order.OrderType)
	}

	return req, nil
}

func mapPredictionSide(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "yes":
		return "yes", nil
	case "no":
		return "no", nil
	default:
		return "", fmt.Errorf("kalshi: prediction side must be YES or NO")
	}
}

func mapOrderStatus(raw string) (domain.OrderStatus, error) {
	switch status := strings.ToLower(strings.TrimSpace(raw)); status {
	case "resting", "open", "pending":
		return domain.OrderStatusSubmitted, nil
	case "executed", "filled":
		return domain.OrderStatusFilled, nil
	case "partially_executed", "partial":
		return domain.OrderStatusPartial, nil
	case "canceled", "cancelled", "cancelled_by_user":
		return domain.OrderStatusCancelled, nil
	case "rejected":
		return domain.OrderStatusRejected, nil
	default:
		return "", fmt.Errorf("kalshi: unsupported order status %q", raw)
	}
}
