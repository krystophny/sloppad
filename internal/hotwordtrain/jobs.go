package hotwordtrain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func (m *Manager) runGeneration(ctx context.Context, models []string, sampleCount int) {
	jobRoot := filepath.Join(m.generatedDir(), timeSafeStamp())
	_ = m.ensureDir(jobRoot)

	totalGenerated := 0
	modelStates := make([]ModelStatus, 0, len(models))
	for _, model := range models {
		modelStates = append(modelStates, ModelStatus{Name: model, State: statusStateIdle, Target: sampleCount})
	}

	for idx, model := range models {
		outputDir := filepath.Join(jobRoot, model)
		scriptPath := m.generatorPath(model)
		state := modelStates[idx]
		state.State = statusStateRunning
		state.OutputDir = outputDir
		m.updateGenerationModel(idx, state, "generating", statusProgressForIndex(idx, len(models)))

		if err := m.ensureDir(outputDir); err != nil {
			state.State = statusStateFailed
			state.Message = err.Error()
			modelStates[idx] = state
			m.finishGenerationModel(idx, state, totalGenerated, false)
			continue
		}
		if err := requireExecutable(scriptPath); err != nil {
			state.State = statusStateFailed
			state.Message = err.Error()
			modelStates[idx] = state
			m.finishGenerationModel(idx, state, totalGenerated, false)
			continue
		}

		lastLine := ""
		cmd := exec.CommandContext(ctx, scriptPath,
			"--recordings-dir", m.recordingsDir(),
			"--output-dir", outputDir,
			"--count", strconv.Itoa(sampleCount),
			"--model-id", model,
		)
		cmd.Env = append(os.Environ(),
			"TABURA_HOTWORD_RECORDINGS_DIR="+m.recordingsDir(),
			"TABURA_HOTWORD_OUTPUT_DIR="+outputDir,
			"TABURA_HOTWORD_FEEDBACK_DIR="+m.feedbackDir(),
			"TABURA_HOTWORD_SAMPLE_COUNT="+strconv.Itoa(sampleCount),
			"TABURA_HOTWORD_MODEL_ID="+model,
		)
		err := runLoggedCommand(ctx, cmd, func(line string) {
			lastLine = line
			state.State = statusStateRunning
			state.Message = line
			m.updateGenerationModel(idx, state, "generating", statusProgressForIndex(idx, len(models)))
		})
		if err != nil {
			state.State = statusStateFailed
			state.Message = formatCommandError(err, lastLine)
			modelStates[idx] = state
			m.finishGenerationModel(idx, state, totalGenerated, false)
			continue
		}
		state.State = statusStateCompleted
		state.Count = countWAVFiles(outputDir)
		state.Message = "Generation complete."
		totalGenerated += state.Count
		modelStates[idx] = state
		m.finishGenerationModel(idx, state, totalGenerated, true)
	}

	modelsSnapshot := m.GenerationStatus().Models
	success := false
	for _, model := range modelsSnapshot {
		if model.State == statusStateCompleted {
			success = true
			break
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	next := cloneStatus(m.generation)
	next.Models = append([]ModelStatus(nil), modelsSnapshot...)
	next.GeneratedSamples = totalGenerated
	next.FinishedAt = nowRFC3339()
	if success {
		next.State = statusStateCompleted
		next.Stage = "complete"
		next.Progress = maxStatusProgress
		next.Message = "Generation complete."
		next.Error = ""
	} else {
		next.State = statusStateFailed
		next.Stage = "failed"
		next.Progress = maxStatusProgress
		next.Error = "Generation failed for every selected model."
		if next.Message == "" {
			next.Message = next.Error
		}
	}
	m.setGenerationLocked(next)
}

func (m *Manager) updateGenerationModel(index int, state ModelStatus, stage string, progress int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := cloneStatus(m.generation)
	if index < len(next.Models) {
		next.Models[index] = state
	}
	next.State = statusStateRunning
	next.Stage = stage
	next.Progress = progress
	next.Message = state.Message
	m.setGenerationLocked(next)
}

func (m *Manager) finishGenerationModel(index int, state ModelStatus, totalGenerated int, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := cloneStatus(m.generation)
	if index < len(next.Models) {
		next.Models[index] = state
	}
	next.GeneratedSamples = totalGenerated
	next.Stage = "generating"
	next.Progress = statusProgressForIndex(index+1, len(next.Models))
	next.Message = state.Message
	if !success && next.Error == "" {
		next.Error = state.Message
	}
	m.setGenerationLocked(next)
}

func (m *Manager) runTraining(ctx context.Context, req TrainRequest) {
	outputDir := m.modelsDir()
	_ = m.ensureDir(outputDir)
	run, err := m.prepareTrainingRun(req)
	if err != nil {
		m.mu.Lock()
		next := cloneStatus(m.training)
		next.State = statusStateFailed
		next.Stage = "failed"
		next.Progress = maxStatusProgress
		next.Error = err.Error()
		next.Message = err.Error()
		next.FinishedAt = nowRFC3339()
		m.setTrainingLocked(next)
		m.mu.Unlock()
		return
	}

	m.mu.Lock()
	next := cloneStatus(m.training)
	next.Stage = "preparing"
	next.Progress = 5
	next.Message = "Preparing training pipeline."
	m.setTrainingLocked(next)
	m.mu.Unlock()

	lastLine := ""
	lastLine, err = m.runTrainingCommand(ctx, outputDir, run.ConfigPath, nil, func(line string) {
		m.mu.Lock()
		defer m.mu.Unlock()
		next := cloneStatus(m.training)
		next.State = statusStateRunning
		next.Stage = "training"
		next.Progress = 55
		next.Message = line
		m.setTrainingLocked(next)
	})

	models, listErr := m.ListModels()
	latest := newestModel(models)
	m.mu.Lock()
	defer m.mu.Unlock()
	next = cloneStatus(m.training)
	next.FinishedAt = nowRFC3339()
	next.LatestModel = latest
	if err != nil {
		next.State = statusStateFailed
		next.Stage = "failed"
		next.Progress = maxStatusProgress
		next.Error = formatCommandError(err, lastLine)
		next.Message = next.Error
		m.setTrainingLocked(next)
		return
	}
	if listErr == nil && latest != "" {
		next.LatestModel = latest
	}
	next.State = statusStateCompleted
	next.Stage = "complete"
	next.Progress = maxStatusProgress
	if lastLine != "" {
		next.Message = lastLine
	} else {
		next.Message = "Training complete."
	}
	next.Error = ""
	m.setTrainingLocked(next)
}

func (m *Manager) StartPipeline(ctx context.Context, req PipelineRequest) error {
	m.mu.Lock()
	if m.training.State == statusStateRunning {
		m.mu.Unlock()
		return errors.New("training already running")
	}
	m.setTrainingLocked(Status{
		State:     statusStateRunning,
		Stage:     "queued",
		Message:   "Queued guided pipeline.",
		Progress:  1,
		StartedAt: nowRFC3339(),
		UpdatedAt: nowRFC3339(),
	})
	m.mu.Unlock()

	go m.runPipeline(ctx, req)
	return nil
}

func (m *Manager) runPipeline(ctx context.Context, req PipelineRequest) {
	settings := m.SettingsSnapshot()
	trainReq := TrainRequest{
		SampleCount:     settings.SampleCount,
		NegativePhrases: settings.NegativePhrases,
	}
	run, err := m.prepareTrainingRun(trainReq)
	if err != nil {
		m.failTrainingPipeline(err)
		return
	}

	selectedModels := normalizeModels(req.Models)
	if len(selectedModels) == 0 {
		preferred := normalizeModelID(settings.PreferredGenerator)
		if preferred != "" && preferred != "piper" {
			selectedModels = []string{preferred}
		}
	}
	generatedDirs := make([]string, 0, len(selectedModels))
	if len(selectedModels) > 0 {
		m.updateTrainingPipeline("voice-clone-generation", 10, fmt.Sprintf("Generating cloned samples with %s.", strings.Join(selectedModels, ", ")))
		m.runGeneration(ctx, selectedModels, settings.SampleCount)
		for _, model := range m.GenerationStatus().Models {
			if model.State == statusStateCompleted && strings.TrimSpace(model.OutputDir) != "" {
				generatedDirs = append(generatedDirs, model.OutputDir)
			}
		}
	}

	m.updateTrainingPipeline("trainer-resolve", 40, "Preparing the openWakeWord trainer workspace.")
	if _, err := m.runTrainingCommand(ctx, m.modelsDir(), run.ConfigPath, []string{"--step", "resolve-config"}, func(line string) {
		m.updateTrainingPipeline("trainer-resolve", 45, line)
	}); err != nil {
		m.failTrainingPipeline(err)
		return
	}
	if _, err := m.runTrainingCommand(ctx, m.modelsDir(), run.ConfigPath, []string{"--step", "generate"}, func(line string) {
		m.updateTrainingPipeline("trainer-generate", 58, line)
	}); err != nil {
		m.failTrainingPipeline(err)
		return
	}

	summary, err := m.stageTrainingDataset(run, generatedDirs)
	if err != nil {
		m.failTrainingPipeline(err)
		return
	}
	m.updateTrainingPipeline("trainer-stage", 68, fmt.Sprintf("Staged %d/%d positive and %d/%d negative clips.", summary.PositiveTrain, summary.PositiveTest, summary.NegativeTrain, summary.NegativeTest))

	lastLine, err := m.runTrainingCommand(ctx, m.modelsDir(), run.ConfigPath, []string{"--from", "augment"}, func(line string) {
		m.updateTrainingPipeline("trainer-train", 82, line)
	})
	if err != nil {
		m.failTrainingPipeline(formatPipelineCommandError(err, lastLine))
		return
	}

	models, listErr := m.ListModels()
	latest := newestModel(models)
	if settings.AutoDeploy && latest != "" {
		m.updateTrainingPipeline("deploy", 95, fmt.Sprintf("Deploying %s.", latest))
		if _, err := m.DeployModel(latest); err != nil {
			m.failTrainingPipeline(err)
			return
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	next := cloneStatus(m.training)
	next.FinishedAt = nowRFC3339()
	next.LatestModel = latest
	if listErr == nil && latest != "" {
		next.LatestModel = latest
	}
	next.State = statusStateCompleted
	next.Stage = "complete"
	next.Progress = maxStatusProgress
	if settings.AutoDeploy && latest != "" {
		next.Message = fmt.Sprintf("Training complete and deployed %s.", latest)
	} else if lastLine != "" {
		next.Message = lastLine
	} else {
		next.Message = "Training complete."
	}
	next.Error = ""
	m.setTrainingLocked(next)
}

func (m *Manager) updateTrainingPipeline(stage string, progress int, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := cloneStatus(m.training)
	next.State = statusStateRunning
	next.Stage = stage
	next.Progress = progress
	next.Message = strings.TrimSpace(message)
	m.setTrainingLocked(next)
}

func (m *Manager) failTrainingPipeline(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := cloneStatus(m.training)
	next.State = statusStateFailed
	next.Stage = "failed"
	next.Progress = maxStatusProgress
	next.Error = strings.TrimSpace(err.Error())
	next.Message = next.Error
	next.FinishedAt = nowRFC3339()
	m.setTrainingLocked(next)
}

func (m *Manager) runTrainingCommand(ctx context.Context, outputDir, configPath string, extraArgs []string, onLine func(string)) (string, error) {
	scriptPath := m.trainingScriptPath()
	cmd := exec.CommandContext(ctx, scriptPath, extraArgs...)
	cmd.Env = append(os.Environ(),
		"TABURA_HOTWORD_OUTPUT_DIR="+outputDir,
		"TABURA_HOTWORD_RECORDINGS_DIR="+m.recordingsDir(),
		"TABURA_HOTWORD_FEEDBACK_DIR="+m.feedbackDir(),
		"TABURA_HOTWORD_CONFIG="+configPath,
	)
	lastLine := ""
	err := runLoggedCommand(ctx, cmd, func(line string) {
		lastLine = line
		if onLine != nil {
			onLine(line)
		}
	})
	return lastLine, err
}

func formatPipelineCommandError(err error, lastLine string) error {
	if lastLine != "" {
		return errors.New(lastLine)
	}
	return err
}

func timeSafeStamp() string {
	return strings.NewReplacer(":", "-", ".", "-", "T", "_").Replace(nowRFC3339())
}
