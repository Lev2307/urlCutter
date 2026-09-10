package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	api "github.com/Lev2307/urlCutter/internal/api"
	config "github.com/Lev2307/urlCutter/internal/config"
	db "github.com/Lev2307/urlCutter/internal/db"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// создание нового экзмепляра логгера + установка уровня лога
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbPool, err := db.NewDatabase(ctx, cfg.DBName)
	if err != nil {
		logger.Error("open db pool", "error", err)
		return
	}
	defer dbPool.Close()

	storage := db.NewSQLiteStorage(dbPool)

	srv := api.NewServer(storage, logger, cfg)
	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Routes(),
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	srvErr := make(chan error, 1)
	go func() {
		srvErr <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-srvErr:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
		}
		return
	case <-ctx.Done():
	}

	logger.Info("shutting down successfully...")

	shutDownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpSrv.Shutdown(shutDownCtx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
	}

	logger.Info("Server stopped cleanly...")
}

/*
=============================================================================
 Как устроено выключение сервера — шпаргалка на будущее
=============================================================================

--- Три горутины ---

  1. main                — эта функция.
  2. Горутина сигналов   — заведена внутри signal.NotifyContext.
                           Ждёт SIGINT (Ctrl+C) или SIGTERM (kill, docker stop,
                           systemd при рестарте) и, дождавшись, зовёт cancel().
                           Оба сигнала приходят СНАРУЖИ и значат «выключайся
                           по-хорошему». Успешный выход программы сигналов не шлёт.
  3. Горутина сервера    — та, что с ListenAndServe. Внутри: открывается listener
                           на порту, дальше вечный цикл Accept(). На КАЖДОЕ
                           входящее соединение запускается ещё одна отдельная
                           горутина — она и зовёт хендлер.

Программа живёт ровно столько, сколько живёт main. Как только main возвращается,
процесс умирает и все горутины гибнут мгновенно, без defer. Горутина сервера
сама по себе процесс живым НЕ держит.
Поэтому main обязана где-то встать и ждать — это и есть <-ctx.Done().
Если убрать ту строку, main сразу пойдёт в Shutdown и сервер проживёт миллисекунду.

--- Что такое <-ctx.Done() ---

  ctx.Done()  — просто отдаёт канал. Мгновенно, ничего не блокирует.
  <-          — вот ЭТО ожидание.

Канал принадлежит контексту: его создаёт context.WithCancel внутри NotifyContext,
ещё до запуска горутины сигналов. Горутина им не владеет.
В канал НИКТО НИЧЕГО НЕ ПИШЕТ — его ЗАКРЫВАЮТ.

Два правила про каналы, из которых всё складывается:
  - чтение из открытого пустого канала -> блокирует, горутина спит (0% CPU);
  - чтение из закрытого канала         -> возвращается мгновенно, всегда.

Значит: пока канал открыт, main спит на <-ctx.Done(). cancel() закрывает канал —
и та же строка мгновенно возвращает нулевое значение. Значение никому не нужно,
поэтому его даже не присваивают в переменную. main едет дальше.

Отмена сделана закрытием, а не отправкой, чтобы разбудить ВСЕХ ждущих разом.
Отправленное значение досталось бы только одной горутине.

--- Порядок выключения ---

  1. Ctrl+C -> горутина сигналов зовёт cancel() -> КАНАЛ ЗАКРЫТ.
  2. Просыпается ТОЛЬКО main. Сервер в этот момент ещё жив и принимает запросы.
  3. main создаёт shutDownCtx и зовёт httpSrv.Shutdown.
  4. Shutdown закрывает listener -> ListenAndServe возвращает http.ErrServerClosed
     -> горутина сервера доходит до конца и завершается САМА.
     ErrServerClosed — штатное завершение, а не сбой; поэтому её и отсеивают
     в проверке после ListenAndServe.
  5. Shutdown ЖДЁТ, пока догорят уже принятые запросы.
     Новые в это время не принимаются — клиент получит connection refused.
  6. Shutdown вернулся -> лог -> main дошла до конца -> процесс вышел.

ВАЖНО: <-ctx.Done() НИКОГО НЕ ВЫКЛЮЧАЕТ. Оно действует только на ту горутину,
которая его выполняет, то есть на main. Горутину сервера гасит Shutdown, закрывая
listener. Убить чужую горутину в Go нельзя вообще никак — можно только попросить
её завершиться, и она должна согласиться сама.

--- Про таймаут в Shutdown ---

shutDownCtx — это НЕ «подождать 10 секунд перед выходом».
Это ПРЕДЕЛ ТЕРПЕНИЯ: сколько максимум ждать недоделанные запросы.
  - запросы догорели за 3 с   -> Shutdown вернул nil через 3 с, лимит не понадобился;
  - запросы висят дольше 10 с -> Shutdown бросает их и возвращает
                                 context.DeadlineExceeded (ветка с логом об ошибке).

defer stop() снимает перехват сигнала обратно. Нужен, чтобы второй Ctrl+C убил
процесс по-настоящему, если Shutdown вдруг подвиснет.

--- Про таймауты сервера ---

Меряют РАЗНЫЕ отрезки жизни запроса:
  ReadTimeout  — от принятия соединения до конца чтения тела;
  WriteTimeout — примерно от конца чтения заголовков до конца записи ответа;
  IdleTimeout  — простой между запросами в keep-alive (при нуле берётся ReadTimeout).
У функции http.ListenAndServe (не метода) все они нулевые, то есть бесконечные:
одно медленное соединение висит вечно и держит горутину (атака Slowloris).
Поэтому сервер и собран структурой &http.Server{...}.

--- Как проверить, что graceful реально работает ---

Временный хендлер с time.Sleep(3 * time.Second); дёрнуть его через curl.exe
(в PowerShell голое curl — это алиас на Invoke-WebRequest, не то) и, пока он висит,
нажать Ctrl+C. Ожидаемое: между двумя логами ~3 секунды, а curl получает
нормальный ответ. Если у логов ОДИНАКОВАЯ метка времени — ждать было нечего,
и graceful-часть ни разу не сработала.
=============================================================================
*/
