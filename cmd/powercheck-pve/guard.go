package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"powercheck/internal/core"
	"powercheck/internal/dryrun"
	"powercheck/internal/guardevents"
	"powercheck/internal/guardstate"
	"powercheck/internal/outageconfig"
	"powercheck/internal/pveexec"
	"powercheck/internal/pvereader"
)

type guardExecutor interface {
	Status(context.Context) (pveexec.Result, error)
	StopAll(context.Context) (pveexec.Result, error)
	ForceStopGuest(context.Context, int) (pveexec.Result, error)
	PoweroffHost(context.Context) (pveexec.Result, error)
}

type guardConfigStore interface {
	Load() (outageconfig.Config, error)
}

type shutdownTimeoutSetter interface {
	SetShutdownTimeout(time.Duration)
}

func runAutoGuard(
	ctx context.Context,
	config dryrun.Config,
	configRevision int,
	configStore guardConfigStore,
	stateStore guardstate.Store,
	eventStore *guardevents.Store,
	collector dryrun.SnapshotCollector,
	executor guardExecutor,
	logger *log.Logger,
) error {
	if collector == nil {
		return fmt.Errorf("automatic guard collector is required")
	}
	if executor == nil {
		return fmt.Errorf("automatic guard executor is required")
	}
	if logger == nil {
		return fmt.Errorf("automatic guard logger is required")
	}
	if configStore == nil {
		return fmt.Errorf("automatic guard config store is required")
	}
	engine, restoredDetection, started, restoredRevision, restored, err := stateStore.Restore()
	if err != nil {
		return err
	}
	if restored {
		config.Detection = restoredDetection
		configRevision = restoredRevision
		logger.Printf(
			"restored state=%s revision=%d outage_age=%s",
			engine.Status().State,
			configRevision,
			time.Since(started).Round(time.Second),
		)
	} else {
		engine, err = core.NewEngine(config.Detection)
		if err != nil {
			return err
		}
		started = time.Now()
	}

	ticker := time.NewTicker(config.Detection.Interval)
	defer ticker.Stop()
	logger.Printf(
		"armed node=%s interval=%s confirm=%s graceful=%ds emergency_at=%s total_budget=%s",
		config.PVENode,
		config.Detection.Interval,
		config.Detection.NUTConfirm,
		config.GuestShutdownTimeoutSeconds,
		config.Detection.TotalBudget-config.Detection.EmergencyReserve,
		config.Detection.TotalBudget,
	)
	recordGuardEvent(eventStore, guardevents.Event{
		Type:  "success",
		Title: "PVE 正式守护已启动",
		Note: fmt.Sprintf(
			"配置 r%d，状态 %s，%s 确认后由 PVE 本机执行保护",
			configRevision,
			engine.Status().State,
			config.Detection.NUTConfirm,
		),
	}, logger)

	for {
		if engine.Status().State == core.StateNormal {
			stored, loadErr := configStore.Load()
			if loadErr != nil {
				logger.Printf("configuration reload failed error=%q", loadErr.Error())
			} else if stored.Mode != outageconfig.ModeProduction {
				logger.Printf("configuration reload ignored mode=%q", stored.Mode)
			} else if stored.Revision != configRevision {
				next := config
				next.Detection = stored.Detection()
				next.GuestShutdownTimeoutSeconds = stored.GuestShutdownTimeoutSeconds
				if validateErr := next.Validate(); validateErr != nil {
					logger.Printf("configuration reload rejected revision=%d error=%q", stored.Revision, validateErr.Error())
				} else {
					nextEngine, engineErr := core.NewEngine(next.Detection)
					if engineErr != nil {
						return engineErr
					}
					config = next
					if setter, ok := executor.(shutdownTimeoutSetter); ok {
						setter.SetShutdownTimeout(time.Duration(config.GuestShutdownTimeoutSeconds) * time.Second)
					}
					configRevision = stored.Revision
					engine = nextEngine
					started = time.Now()
					ticker.Reset(config.Detection.Interval)
					logger.Printf("configuration applied revision=%d", configRevision)
					recordGuardEvent(eventStore, guardevents.Event{
						Type:  "success",
						Title: "PVE 正式守护参数已更新",
						Note:  fmt.Sprintf("已应用配置 r%d", configRevision),
					}, logger)
				}
			}
		}
		report := collector.Collect(ctx)
		at := time.Since(started)
		actions, stepErr := engine.Step(at, report.Snapshot)
		if stepErr != nil {
			return stepErr
		}
		for _, issue := range report.Issues {
			logger.Printf("sample issue source=%s error=%q", issue.Source, issue.Message)
		}
		emergencyDeadline, totalDeadline := guardDeadlines(started, config.Detection, engine.Status())
		for _, action := range actions {
			logger.Printf(
				"action kind=%s at=%s reason=%q nut=%s lan=%t wan=%t guests_stopped=%t",
				action.Kind,
				action.At.Round(time.Second),
				action.Reason,
				report.Snapshot.NUT,
				report.Snapshot.LANReachable,
				report.Snapshot.WANReachable,
				report.Snapshot.AllGuestsStopped,
			)
			recordGuardAction(eventStore, action, logger)
			if err := executeGuardAction(
				ctx,
				action,
				executor,
				config,
				emergencyDeadline,
				totalDeadline,
				logger,
			); err != nil {
				return fmt.Errorf("execute automatic guard action %s: %w", action.Kind, err)
			}
		}
		if err := stateStore.Save(started, configRevision, config.Detection, engine); err != nil {
			return fmt.Errorf("persist automatic guard state: %w", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func recordGuardAction(store *guardevents.Store, action core.Action, logger *log.Logger) {
	event := guardevents.Event{Type: "info", Note: action.Reason}
	switch action.Kind {
	case core.ActionOutageCandidateStarted:
		event.Type = "warning"
		event.Title = "PVE 检测到断电候选"
	case core.ActionOutageCancelled:
		event.Type = "success"
		event.Title = "PVE 断电候选已取消"
	case core.ActionGracefulShutdown:
		event.Type = "warning"
		event.Title = "PVE 开始安全关闭 Guest"
	case core.ActionEmergencyStopRemaining:
		event.Type = "warning"
		event.Title = "PVE 进入紧急停止阶段"
	case core.ActionHostPoweroffRequested:
		event.Type = "warning"
		event.Title = "PVE 已请求关闭宿主机"
	default:
		return
	}
	recordGuardEvent(store, event, logger)
}

func recordGuardEvent(store *guardevents.Store, event guardevents.Event, logger *log.Logger) {
	if store == nil {
		return
	}
	if err := store.Add(event); err != nil {
		logger.Printf("event persistence failed error=%q", err.Error())
	}
}

func executeGuardAction(
	parent context.Context,
	action core.Action,
	executor guardExecutor,
	config dryrun.Config,
	emergencyDeadline *time.Time,
	totalDeadline *time.Time,
	logger *log.Logger,
) error {
	switch action.Kind {
	case core.ActionOutageCandidateStarted, core.ActionOutageCancelled:
		return nil
	case core.ActionGracefulShutdown:
		if setter, ok := executor.(shutdownTimeoutSetter); ok {
			setter.SetShutdownTimeout(time.Duration(config.GuestShutdownTimeoutSeconds) * time.Second)
		}
		timeout := time.Duration(config.GuestShutdownTimeoutSeconds)*time.Second + 20*time.Second
		ctx, cancel, err := boundedContext(parent, timeout, emergencyDeadline)
		if err != nil {
			return err
		}
		defer cancel()
		result, err := executor.StopAll(ctx)
		if err != nil {
			logger.Printf("graceful stop incomplete executed=%t error=%q", result.Executed, err.Error())
			return nil
		}
		logger.Printf("graceful stop complete all_guests_stopped=%t", result.AllGuestsStopped)
		return nil
	case core.ActionEmergencyStopRemaining:
		return forceStopRemaining(parent, executor, totalDeadline, logger)
	case core.ActionHostPoweroffRequested:
		ctx, cancel, err := boundedContext(parent, 10*time.Second, totalDeadline)
		if err != nil {
			return err
		}
		defer cancel()
		result, err := executor.PoweroffHost(ctx)
		if err != nil {
			return fmt.Errorf("host poweroff blocked or failed: %w", err)
		}
		logger.Printf("host poweroff accepted executed=%t", result.Executed)
		return nil
	default:
		return fmt.Errorf("unsupported automatic guard action %q", action.Kind)
	}
}

func forceStopRemaining(
	parent context.Context,
	executor guardExecutor,
	deadline *time.Time,
	logger *log.Logger,
) error {
	ctx, cancel, err := boundedContext(parent, 5*time.Second, deadline)
	if err != nil {
		return err
	}
	status, err := executor.Status(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("read remaining guests: %w", err)
	}
	var guests []pvereader.Guest
	for _, guest := range status.Guests {
		if guest.Template || guest.Status == "stopped" {
			continue
		}
		guests = append(guests, guest)
	}
	var wait sync.WaitGroup
	errors := make(chan error, len(guests))
	for _, guest := range guests {
		guest := guest
		wait.Add(1)
		go func() {
			defer wait.Done()
			forceContext, forceCancel, contextErr := boundedContext(parent, 15*time.Second, deadline)
			if contextErr != nil {
				errors <- contextErr
				return
			}
			defer forceCancel()
			result, forceErr := executor.ForceStopGuest(forceContext, guest.VMID)
			if forceErr != nil {
				errors <- fmt.Errorf("force-stop VMID %d: %w", guest.VMID, forceErr)
				return
			}
			logger.Printf(
				"emergency force-stop vmid=%d type=%s executed=%t",
				guest.VMID,
				guest.Type,
				result.Executed,
			)
		}()
	}
	wait.Wait()
	close(errors)
	for forceErr := range errors {
		return forceErr
	}

	verifyContext, verifyCancel, err := boundedContext(parent, 5*time.Second, deadline)
	if err != nil {
		return err
	}
	defer verifyCancel()
	verified, err := executor.Status(verifyContext)
	if err != nil {
		return fmt.Errorf("verify guests after force-stop: %w", err)
	}
	if !pvereader.AllGuestsStopped(verified.Guests) {
		return fmt.Errorf("one or more guests still run after emergency force-stop")
	}
	return nil
}

func guardDeadlines(origin time.Time, config core.Config, status core.Status) (*time.Time, *time.Time) {
	if status.OutageStartedAt == nil {
		return nil, nil
	}
	emergency := origin.Add(*status.OutageStartedAt + config.TotalBudget - config.EmergencyReserve)
	total := origin.Add(*status.OutageStartedAt + config.TotalBudget)
	return &emergency, &total
}

func boundedContext(parent context.Context, maximum time.Duration, deadline *time.Time) (context.Context, context.CancelFunc, error) {
	target := time.Now().Add(maximum)
	if deadline != nil && deadline.Before(target) {
		target = *deadline
	}
	if !target.After(time.Now()) {
		return nil, nil, fmt.Errorf("automatic guard total budget exhausted")
	}
	ctx, cancel := context.WithDeadline(parent, target)
	return ctx, cancel, nil
}
