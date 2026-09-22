package api

import (
	"context"
	"sync"
	"time"
)

type RateLimiter struct {
	mu      sync.Mutex // не RWMutex: tokens меняется на каждом запросе, даже на отклонённом, — здесь все операции пишущие
	buckets map[string]*bucket
	rps     float64 // скорость наполнения ведра, например, 1 токен в секунду. float64, чтобы выражать медленные лимиты. «Один запрос в две секунды» — это rps = 0.5
	burst   float64 // вместимость ведра. Тут можно и int
	ttl     time.Duration
	now     func() time.Time
}

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

func NewRateLimiter(ctx context.Context, rps, burst float64, ttl time.Duration, now func() time.Time) *RateLimiter {
	if rps <= 0 {
		rps = 1
	}
	if burst < 1 {
		burst = 1
	}
	ttl = time.Duration(max(burst/rps, ttl.Seconds()) * float64(time.Second)) //  max нужен так как ttl не должен быть меньше времени наполнения ведра, то есть burst / rps
	if now == nil {
		now = time.Now
	}

	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		rps:     rps,
		burst:   burst,
		ttl:     ttl,
		now:     now,
	}
	go rl.cleanup(ctx) // в горутине => работает параллельно allow
	return rl
}

func (rl *RateLimiter) cleanup(ctx context.Context) {
	ticker := time.NewTicker(rl.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := rl.now()
			rl.mu.Lock() // карта общая с allow: одновременные range+delete и запись из
			// обработчиков дают fatal error: concurrent map iteration and map write,
			// и это НЕ паника — recover() её не ловит, процесс умирает целиком
			for ip, b := range rl.buckets {
				if now.Sub(b.lastSeen) > rl.ttl {
					delete(rl.buckets, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}

func (rl *RateLimiter) allow(ip string) (bool, time.Duration) {
	// Замок держим на всю функцию: пересчёт токенов — это чтение-изменение-запись,
	// и разорвать его нельзя. Иначе два запроса с одного IP прочитают tokens=1.0,
	// оба решат, что хватает, и пройдут по одному и тому же токену.
	// Под замком только счёт — next.ServeHTTP зовётся снаружи, в middleware.
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	b, ok := rl.buckets[ip] // смотрю нет ли ведра для того же ip
	if !ok {
		b = &bucket{tokens: rl.burst, lastSeen: now}
		rl.buckets[ip] = b
	}
	return b.allow(now, rl.rps, rl.burst) // вызов проверки на возможность черпания из ведра токена
}

func (b *bucket) allow(now time.Time, rps, burst float64) (bool, time.Duration) {
	b.tokens = min(b.tokens+now.Sub(b.lastSeen).Seconds()*rps, burst) // если ведро будет переполнено, то возьмется за минимум burst - сама вместимость ведра
	b.lastSeen = now
	if b.tokens < 1 {
		return false, time.Duration(float64(time.Second) * (1 - b.tokens) / rps) // ждать = нехватка токенов / скорость; ×time.Second переводит секунды в наносекунды
	}
	b.tokens--
	return true, 0
}
