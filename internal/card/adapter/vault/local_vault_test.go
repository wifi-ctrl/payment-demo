package vault

import (
	"context"
	"errors"
	"testing"
	"time"

	"payment-demo/internal/card/domain/model"
	"payment-demo/internal/card/domain/port"
)

func TestLocalVault_PeekCachedCard_Success(t *testing.T) {
	v := NewLocalVault()
	ctx := context.Background()
	tok, err := v.CacheTokenizedCard(ctx, port.CachedCardData{UserID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := v.PeekCachedCard(ctx, tok, "alice")
	if err != nil || data == nil || data.UserID != "alice" {
		t.Fatalf("PeekCachedCard: err=%v data=%v", err, data)
	}
	// 未消费，仍可 Consume
	consumed, err := v.ConsumeCardToken(ctx, tok)
	if err != nil || consumed.UserID != "alice" {
		t.Fatalf("Consume after peek: err=%v user=%s", err, consumed.UserID)
	}
}

func TestLocalVault_PeekCachedCard_WrongUser(t *testing.T) {
	v := NewLocalVault()
	ctx := context.Background()
	tok, err := v.CacheTokenizedCard(ctx, port.CachedCardData{UserID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.PeekCachedCard(ctx, tok, "bob")
	if err == nil {
		t.Fatal("want error for wrong user")
	}
	if !errors.Is(err, model.ErrCardBelongsToOtherUser) {
		t.Fatalf("want ErrCardBelongsToOtherUser, got %v", err)
	}
}

func TestLocalVault_PeekCachedCard_Expired(t *testing.T) {
	v := &LocalVault{
		cache: make(map[string]*cachedEntry),
		ttl:   -1 * time.Hour,
	}
	ctx := context.Background()
	tok, err := v.CacheTokenizedCard(ctx, port.CachedCardData{UserID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.PeekCachedCard(ctx, tok, "alice")
	if err == nil {
		t.Fatal("want error for expired entry")
	}
	if !errors.Is(err, model.ErrCardTokenExpired) {
		t.Fatalf("want ErrCardTokenExpired, got %v", err)
	}
}
