package httpapi_test

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valkey-io/valkey-go"

	"github.com/backplane-labs/backplane-lic/internal/infra/httpapi"
)

func TestValkeyThrottleSharedCounter(t *testing.T) {
	t.Parallel()

	addr := os.Getenv("LIC_TEST_VALKEY_ADDR")
	if addr == "" {
		t.Skip("set LIC_TEST_VALKEY_ADDR to verify the replica-shared limiter")
	}

	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(client.Close)

	first := httpapi.NewValkeyThrottler(client)
	second := httpapi.NewValkeyThrottler(client)
	identity := uuid.NewString()
	window := time.Hour
	key := "lic/throttle/" + strconv.FormatInt(time.Now().Unix()/int64(window.Seconds()), 10) + "/" + identity

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		_ = client.Do(ctx, client.B().Del().Key(key).Build()).Error()
	})

	for attempt := int64(1); attempt <= 6; attempt++ {
		limiter := first
		if attempt%2 == 0 {
			limiter = second
		}

		count, err := limiter.Inc(t.Context(), identity, window)
		if err != nil || count != attempt {
			t.Fatalf("replica-shared attempt %d: count %d, error %v", attempt, count, err)
		}
	}

	ttl, err := client.Do(t.Context(), client.B().Pttl().Key(key).Build()).ToInt64()
	if err != nil || ttl <= 0 || ttl > window.Milliseconds() {
		t.Fatalf("counter expiry: %dms, %v", ttl, err)
	}
}
