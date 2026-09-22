package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Lev2307/urlCutter/internal/config"
)

func TestBucketAllowMethod(t *testing.T) {
	const rps, burst = 1.0, 5.0
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	drain := func(t *testing.T, b *bucket, now time.Time) {
		t.Helper()
		for i := 1; i <= int(burst); i++ {
			ok, _ := b.allow(now, rps, burst)
			if !ok {
				t.Fatalf("запрос %d: ждал пропуск, получил отказ (tokens=%v)", i, b.tokens)
			}
		}
	}
	t.Run(" полное ведро пропускает ровно burst запросов подряд", func(t *testing.T) {
		b := &bucket{tokens: burst, lastSeen: t0}
		now := t0 // время НЕ двигаем: всё происходит в один миг
		drain(t, b, now)

		ok, wait := b.allow(now, rps, burst)
		if ok {
			t.Fatal("шестой запрос прошёл, хотя ведро должно быть пустым")
		}
		if wait != time.Second {
			t.Fatalf("wait: ждал %v, получил %v", time.Second, wait)
		}
	})
	t.Run("за секунду доливается ровно один токен", func(t *testing.T) {
		b := &bucket{tokens: burst, lastSeen: t0}
		drain(t, b, t0)
		now := t0.Add(time.Second)

		if ok, _ := b.allow(now, rps, burst); !ok {
			t.Fatal("после секунды ожидания запрос должен был пройти")
		}

		ok, wait := b.allow(now, rps, burst) // тот же самый миг, второго токена нет
		if ok {
			t.Fatal("второй запрос в тот же миг прошёл, хотя долили только один токен")
		}
		if wait != time.Second {
			t.Fatalf("wait: ждал %v, получил %v", time.Second, wait)
		}
	})

	t.Run("долгий простой не переполняет ведро сверх burst", func(t *testing.T) {
		b := &bucket{tokens: burst, lastSeen: t0}
		drain(t, b, t0)
		now := t0.Add(time.Hour)

		if ok, _ := b.allow(now, rps, burst); !ok {
			t.Fatal("после часа ожидания запрос должен был пройти")
		}

		if b.tokens != burst-1 {
			t.Fatalf("Количество токенов - нужно: %.1f, получил: %.1f", burst-1, b.tokens)
		}
	})

	t.Run("дробный долив копится, а не теряется", func(t *testing.T) {
		b := &bucket{tokens: burst, lastSeen: t0}
		drain(t, b, t0)
		now := t0.Add(time.Millisecond * 500)

		ok, wait := b.allow(now, rps, burst)
		if ok {
			t.Fatal("Второй запрос прошел, хотя в ведро не было долито токенов")
		}
		if wait != 500*time.Millisecond {
			t.Fatalf("wait: ждал %v, получил %v — похоже, дробная часть долива потерялась", 500*time.Millisecond, wait)
		}
	})
}

func TestRateLimiterAllow(t *testing.T) {
	const rps, burst = 1.0, 5.0
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ip1 := "192.168.0.1:8000"
	ip2 := "192.244.15.13:1492"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rl := NewRateLimiter(ctx, rps, burst, 60*time.Second, func() time.Time { return t0 })
	okIP1, waitIP1 := rl.allow(ip1) // allow уменьшает токены на 1 при вызове
	if !okIP1 {
		t.Fatalf("Не было создано записи в карте для этого ip: %s. Нужно подождать: %.1f", ip1, waitIP1.Seconds())
	}

	for i := 2; i <= int(burst); i++ { // i = 2, не 1 так как allow уменьшает токены на 1 при вызове
		if ok, wait := rl.allow(ip1); !ok {
			t.Fatalf("ip1: запрос %d из %v отклонён, хотя ведро должно хватать на весь burst (wait=%v)", i, burst, wait)
		}
	}

	if ok, _ := rl.allow(ip1); ok {
		t.Fatal("ip1: ведро опустошено, следующий запрос обязан быть отклонён")
	}

	if ok, wait := rl.allow(ip2); !ok {
		t.Fatalf("ip2: запрос отклонён, хотя лимит выдолбил другой клиент — у каждого ip своё ведро (wait=%v)", wait)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	rps, burst := 1.0, 1.0
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ip := "203.0.113.7:4242"
	rl := NewRateLimiter(ctx, rps, burst, 60*time.Second, func() time.Time { return t0 })
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := NewServer(nil, logger, config.Config{}, rl)
	handler := srv.Routes()

	// первый запрос
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = ip
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Ожидался http-статус: %d, получил: %d", http.StatusOK, w.Code)
	}

	// повторный запрос
	req2 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req2.RemoteAddr = ip
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("Ожидался http-статус: %d, получил: %d", http.StatusTooManyRequests, w2.Code)
	}
	if got := w2.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After: ждал \"1\", получил %q", got)
	}
}
