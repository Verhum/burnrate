package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Verhum/burnrate/internal/caffeinate"
	"github.com/Verhum/burnrate/internal/config"
	brlog "github.com/Verhum/burnrate/internal/log"
	"github.com/Verhum/burnrate/internal/mcp"
	"github.com/Verhum/burnrate/internal/notify"
	"github.com/Verhum/burnrate/internal/prstatus"
	"github.com/Verhum/burnrate/internal/runner"
	"github.com/Verhum/burnrate/internal/scheduler"
	"github.com/Verhum/burnrate/internal/service"
	"github.com/Verhum/burnrate/internal/store"
	"github.com/Verhum/burnrate/internal/whisper"
	"github.com/Verhum/burnrate/web"
)

type Server struct {
	st     *store.Store
	cfg    config.Config
	sched  *scheduler.Scheduler
	caff   *caffeinate.Manager
	hub    *sseHub
	server *http.Server
	logger *brlog.Logger

	taskSvc    *service.TaskService
	runSvc     *service.RunService
	commentSvc *service.CommentService
	requestSvc *service.RequestService
	captureSvc *service.CaptureService

	prober  *prstatus.Prober
	whisper *whisper.Service
}

type worktreeCleanerAdapter struct {
	logger *brlog.Logger
}

func (a *worktreeCleanerAdapter) CheckpointAndRemove(ctx context.Context, worktreePath, repoPath string, runID int64) {
	runner.CheckpointAndRemove(ctx, worktreePath, repoPath, runID, a.logger)
}

func New(st *store.Store, cfg config.Config, sched *scheduler.Scheduler, caff *caffeinate.Manager, whisperSvc *whisper.Service, logger *brlog.Logger) *Server {
	cleaner := &worktreeCleanerAdapter{logger: logger}

	// The request service is built before the struct literal: the task service
	// needs it to retire a finished task's pending requests.
	requestSvc := service.NewRequestService(st, st, st, st, st, sched)

	s := &Server{
		st:      st,
		cfg:     cfg,
		sched:   sched,
		caff:    caff,
		hub:     newSSEHub(),
		logger:  logger,
		whisper: whisperSvc,
		taskSvc: service.NewTaskService(st, st, cleaner, sched, requestSvc),
		runSvc: service.NewRunService(st, st, sched, service.RunServiceConfig{
			DataDir:         cfg.DataDir,
			ClaudeConfigDir: cfg.ClaudeConfigDir,
			TokenCommand:    config.SelfExe() + " token",
		}),
		commentSvc: service.NewCommentService(st, st, st, sched),
		requestSvc: requestSvc,
		captureSvc: service.NewCaptureService(st, st, st, st, st, cfg.DataDir),
	}

	s.prober = prstatus.New(st, logger)
	s.prober.OnChange = s.broadcastTasks

	// Requests broadcast from the service, not from the HTTP handlers: MCP tool
	// calls create requests too, and hanging the broadcast off the handlers made
	// every agent-created request invisible to the UI and the tray.
	requestSvc.SetOnChange(func() {
		s.broadcastRequests()
		s.hub.broadcast("status", s.statusPayload())
	})

	// Route notifications through the SSE hub so the Tauri desktop app can
	// display them as native macOS notifications attributed to "Burnrate".
	notify.SetNotifyFunc(func(n notify.Notification) error {
		s.hub.broadcast("notification", n)
		return nil
	})

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/tasks", s.handleListTasks)
	mux.HandleFunc("POST /api/tasks", s.handleCreateTask)
	mux.HandleFunc("GET /api/tasks/stats", s.handleTaskStats)
	mux.HandleFunc("POST /api/tasks/reorder", s.handleReorderTasks)
	mux.HandleFunc("PUT /api/tasks/{id}", s.handleUpdateTask)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.handleDeleteTask)
	mux.HandleFunc("POST /api/tasks/{id}/pause", s.handlePauseTask)
	mux.HandleFunc("POST /api/tasks/{id}/resume", s.handleResumeTask)
	mux.HandleFunc("POST /api/tasks/{id}/complete", s.handleCompleteTask)
	mux.HandleFunc("POST /api/tasks/{id}/dismiss", s.handleDismissTask)
	mux.HandleFunc("POST /api/tasks/{id}/status", s.handleSetTaskStatus)
	mux.HandleFunc("POST /api/tasks/{id}/checkout", s.handleCheckoutTask)
	mux.HandleFunc("POST /api/tasks/{id}/prs/refresh", s.handleRefreshTaskPRs)
	mux.HandleFunc("GET /api/tasks/{id}/comments", s.handleListComments)
	mux.HandleFunc("POST /api/tasks/{id}/comments", s.handleAddComment)
	mux.HandleFunc("GET /api/tasks/{id}/attachments", s.handleListAttachments)
	mux.HandleFunc("POST /api/tasks/{id}/attachments", s.handleUploadAttachment)
	mux.HandleFunc("GET /api/attachments/{id}/data", s.handleServeAttachment)
	mux.HandleFunc("DELETE /api/attachments/{id}", s.handleDeleteAttachment)

	mux.HandleFunc("GET /api/runs", s.handleListRuns)
	mux.HandleFunc("GET /api/runs/{id}/log", s.handleRunLog)
	mux.HandleFunc("GET /api/runs/{id}/events", s.handleRunEvents)
	mux.HandleFunc("GET /api/runs/{id}/resume", s.handleRunResume)
	mux.HandleFunc("POST /api/runs/{id}/cancel", s.handleCancelRun)
	mux.HandleFunc("POST /api/tasks/{id}/run-now", s.handleRunNow)

	mux.HandleFunc("GET /api/accounts", s.handleListAccounts)
	mux.HandleFunc("POST /api/accounts/select", s.handleSelectAccount)

	mux.HandleFunc("GET /api/models", s.handleListModels)
	mux.HandleFunc("GET /api/usage", s.handleUsage)
	mux.HandleFunc("GET /api/usage/history", s.handleUsageHistory)
	mux.HandleFunc("GET /api/usage/leaderboard", s.handleLeaderboard)
	mux.HandleFunc("GET /api/usage/cost-efficiency", s.handleCostEfficiency)
	mux.HandleFunc("GET /api/usage/streak", s.handleStreak)
	mux.HandleFunc("GET /api/usage/achievements", s.handleAchievements)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handlePutConfig)

	mux.HandleFunc("GET /api/voice/status", s.handleVoiceStatus)
	mux.HandleFunc("POST /api/voice/install", s.handleVoiceInstall)
	mux.HandleFunc("POST /api/voice/transcribe", s.handleTranscribe)
	mux.HandleFunc("POST /api/voice/task", s.handleVoiceTask)
	mux.HandleFunc("POST /api/voice/open", s.handleVoiceOpen)

	mux.HandleFunc("GET /api/caffeinate", s.handleGetCaffeinate)
	mux.HandleFunc("POST /api/caffeinate", s.handleToggleCaffeinate)

	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /health", s.handleHealth)

	mcpServer := mcp.New(s.requestSvc, s.captureSvc)
	mux.Handle("GET /mcp", mcpServer)
	mux.Handle("POST /mcp", mcpServer)

	mux.HandleFunc("POST /api/requests", s.handleCreateRequest)
	mux.HandleFunc("GET /api/requests", s.handleListRequests)
	mux.HandleFunc("GET /api/requests/{id}", s.handleGetRequest)
	mux.HandleFunc("GET /api/requests/{id}/await", s.handleAwaitRequest)
	mux.HandleFunc("POST /api/requests/{id}/respond", s.handleRespondRequest)
	mux.HandleFunc("POST /api/requests/{id}/approve", s.handleApproveRequest)
	mux.HandleFunc("POST /api/requests/{id}/deny", s.handleDenyRequest)

	mux.HandleFunc("POST /api/captures", s.handleCreateCapture)
	mux.HandleFunc("GET /api/captures", s.handleListCaptures)
	mux.HandleFunc("GET /api/captures/{id}", s.handleGetCapture)
	mux.HandleFunc("GET /api/captures/{id}/video", s.handleCaptureVideo)
	mux.HandleFunc("POST /api/captures/{id}/finish", s.handleFinishCapture)
	mux.HandleFunc("POST /api/captures/{id}/notes", s.handleSetCaptureNotes)

	mux.Handle("GET /", web.Handler())

	s.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", cfg.Port),
		Handler: originGuard(cfg.Port)(mux),
	}

	sched.OnBroadcast = func(event string, payload any) {
		switch event {
		case "usage":
			s.hub.broadcast("usage", payload)
			s.hub.broadcast("status", s.statusPayload())
		case "run_complete":
			s.broadcastRuns()
			s.broadcastTasks()
			s.hub.broadcast("status", s.statusPayload())
			if caff != nil {
				caff.SetAutomatic(sched.Status().RunningCount)
				s.hub.broadcast("caffeinate", caff.Status())
			}
		case "run_update":
			s.broadcastRuns()
			s.broadcastTasks()
			s.hub.broadcast("status", s.statusPayload())
			if caff != nil {
				caff.SetAutomatic(sched.Status().RunningCount)
				s.hub.broadcast("caffeinate", caff.Status())
			}
		}
	}

	return s
}

// Prober keeps cached PR states in step with GitHub. The daemon runs its sweep;
// the HTTP handler triggers an immediate refresh for one task.
func (s *Server) Prober() *prstatus.Prober { return s.prober }

func (s *Server) broadcastTasks() {
	tasks, err := s.st.ListTasks()
	if err != nil {
		return
	}
	if tasks == nil {
		tasks = []store.Task{}
	}
	s.hub.broadcast("task", tasks)
}

func (s *Server) broadcastRuns() {
	runs, err := s.st.ListRuns(0, 50)
	if err != nil {
		return
	}
	if runs == nil {
		runs = []store.Run{}
	}
	s.hub.broadcast("run", runs)
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.server.Shutdown(shutdownCtx)
	}()
	err := s.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
