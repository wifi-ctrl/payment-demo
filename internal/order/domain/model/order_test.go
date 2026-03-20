package model_test

import (
	"testing"

	"payment-demo/internal/order/domain/model"
)

func newTestOrder() *model.Order {
	return model.NewOrder("user-1", "merchant-1", "prod-1", "Widget",
		model.PriceBreakdown{
			OriginalAmount: model.NewMoney(10000, "USD"),
			DiscountAmount: model.NewMoney(1000, "USD"),
			TaxAmount:      model.NewMoney(900, "USD"),
			FinalAmount:    model.NewMoney(9900, "USD"),
		}, "cpn-1")
}

func TestNewOrder_CreatesWithPendingPayment(t *testing.T) {
	o := newTestOrder()
	if o.Status != model.OrderStatusPendingPayment {
		t.Errorf("want PENDING_PAYMENT, got %s", o.Status)
	}
	if o.UserID != "user-1" {
		t.Errorf("want user-1, got %s", o.UserID)
	}
	if o.Price.FinalAmount.Amount != 9900 {
		t.Errorf("want 9900, got %d", o.Price.FinalAmount.Amount)
	}
	if o.CouponID != "cpn-1" {
		t.Errorf("want cpn-1, got %s", o.CouponID)
	}
	if len(o.Events) != 1 {
		t.Fatalf("want 1 event, got %d", len(o.Events))
	}
	if o.Events[0].EventName() != "order.created" {
		t.Errorf("want order.created, got %s", o.Events[0].EventName())
	}
}

func TestOrder_MarkAuthorized(t *testing.T) {
	o := newTestOrder()
	o.ClearEvents()
	if err := o.MarkAuthorized("txn-1"); err != nil {
		t.Fatal(err)
	}
	if o.Status != model.OrderStatusAuthorized {
		t.Errorf("want AUTHORIZED, got %s", o.Status)
	}
	if o.TransactionID != "txn-1" {
		t.Errorf("want txn-1, got %s", o.TransactionID)
	}
}

func TestOrder_MarkAuthorized_InvalidState(t *testing.T) {
	o := newTestOrder()
	_ = o.MarkAuthorized("txn-1")
	err := o.MarkAuthorized("txn-2")
	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
}

func TestOrder_MarkPaid(t *testing.T) {
	o := newTestOrder()
	_ = o.MarkAuthorized("txn-1")
	o.ClearEvents()
	if err := o.MarkPaid(); err != nil {
		t.Fatal(err)
	}
	if o.Status != model.OrderStatusPaid {
		t.Errorf("want PAID, got %s", o.Status)
	}
	if o.PaidAt == nil {
		t.Error("PaidAt must be set")
	}
	if len(o.Events) != 1 || o.Events[0].EventName() != "order.paid" {
		t.Errorf("want order.paid event, got %v", o.Events)
	}
}

func TestOrder_MarkPaid_InvalidState(t *testing.T) {
	o := newTestOrder()
	err := o.MarkPaid()
	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
}

func TestOrder_MarkRefunded(t *testing.T) {
	o := newTestOrder()
	_ = o.MarkAuthorized("txn-1")
	_ = o.MarkPaid()
	o.ClearEvents()
	if err := o.MarkRefunded(); err != nil {
		t.Fatal(err)
	}
	if o.Status != model.OrderStatusRefunded {
		t.Errorf("want REFUNDED, got %s", o.Status)
	}
	if len(o.Events) != 1 || o.Events[0].EventName() != "order.refunded" {
		t.Errorf("want order.refunded event, got %v", o.Events)
	}
}

func TestOrder_MarkRefunded_FromAuthorized_Fails(t *testing.T) {
	o := newTestOrder()
	_ = o.MarkAuthorized("txn-1")
	err := o.MarkRefunded()
	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
}

func TestOrder_MarkFailed(t *testing.T) {
	o := newTestOrder()
	if err := o.MarkFailed(); err != nil {
		t.Fatal(err)
	}
	if o.Status != model.OrderStatusFailed {
		t.Errorf("want FAILED, got %s", o.Status)
	}
}

func TestOrder_MarkFailed_InvalidState(t *testing.T) {
	o := newTestOrder()
	_ = o.MarkAuthorized("txn-1")
	err := o.MarkFailed()
	if err != model.ErrInvalidStateTransition {
		t.Errorf("want ErrInvalidStateTransition, got %v", err)
	}
}

func TestOrder_FullStateMachine(t *testing.T) {
	o := newTestOrder()
	if err := o.MarkAuthorized("txn-1"); err != nil {
		t.Fatal(err)
	}
	if err := o.MarkPaid(); err != nil {
		t.Fatal(err)
	}
	if err := o.MarkRefunded(); err != nil {
		t.Fatal(err)
	}
	if o.Status != model.OrderStatusRefunded {
		t.Errorf("want REFUNDED, got %s", o.Status)
	}
}
