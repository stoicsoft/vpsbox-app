package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/stoicsoft/vpsbox/internal/scenario"
)

const labStateVersion = 1

type LabProgress func(string)

func (m *Manager) ValidateLab(path string) (scenario.Manifest, error) {
	return scenario.Load(path)
}

func (m *Manager) ApplyLab(
	ctx context.Context,
	manifestPath string,
	runID string,
	progress LabProgress,
) (*scenario.LabState, error) {
	if err := scenario.ValidateRunID(runID); err != nil {
		return nil, err
	}
	manifest, err := scenario.Load(manifestPath)
	if err != nil {
		return nil, err
	}
	absManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return nil, err
	}

	state, err := m.loadOrCreateLabState(runID, manifest, absManifestPath)
	if err != nil {
		return nil, err
	}
	if state.ManifestName != manifest.Metadata.Name {
		return nil, fmt.Errorf("run %q already belongs to manifest %q", runID, state.ManifestName)
	}
	state.Status = scenario.LabApplying
	state.Error = ""
	state.UpdatedAt = time.Now().UTC()
	if err := m.saveLabState(state); err != nil {
		return nil, err
	}

	report := func(message string) {
		if progress != nil {
			progress(message)
		}
	}

	var stateMu sync.Mutex
	var firstErr error
	var errMu sync.Mutex
	semaphore := make(chan struct{}, manifest.Spec.MaxParallel)
	var wait sync.WaitGroup

	for _, instanceSpec := range manifest.Spec.Instances {
		instanceSpec := instanceSpec
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				errMu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				errMu.Unlock()
				return
			}

			instanceName, nameErr := scenario.InstanceName(runID, instanceSpec.Name)
			if nameErr != nil {
				m.recordLabInstanceError(state, &stateMu, instanceSpec.Name, nameErr)
				errMu.Lock()
				if firstErr == nil {
					firstErr = nameErr
				}
				errMu.Unlock()
				return
			}
			report(fmt.Sprintf("Creating %s (%s)", instanceName, instanceSpec.Role))
			instance, upErr := m.Up(ctx, UpOptions{
				Name:       instanceName,
				CPUs:       instanceSpec.Resources.CPUs,
				MemoryGB:   instanceSpec.Resources.MemoryGB,
				DiskGB:     instanceSpec.Resources.DiskGB,
				Image:      instanceSpec.Image,
				User:       "root",
				SelfSigned: true,
				Progress:   report,
			})
			if upErr != nil {
				m.recordLabInstanceError(state, &stateMu, instanceSpec.Name, upErr)
				errMu.Lock()
				if firstErr == nil {
					firstErr = upErr
				}
				errMu.Unlock()
				return
			}

			instance.ScenarioID = runID
			instance.ScenarioRole = instanceSpec.Role
			instance.Labels = appendUniqueStrings(
				instance.Labels,
				"lab",
				"scenario:"+runID,
				"role:"+instanceSpec.Role,
			)
			if saveErr := m.store.UpsertInstance(*instance); saveErr != nil {
				m.recordLabInstanceError(state, &stateMu, instanceSpec.Name, saveErr)
				errMu.Lock()
				if firstErr == nil {
					firstErr = saveErr
				}
				errMu.Unlock()
				return
			}

			stateMu.Lock()
			for i := range state.Instances {
				if state.Instances[i].LogicalName == instanceSpec.Name {
					state.Instances[i].InstanceName = instance.Name
					state.Instances[i].Host = instance.Host
					state.Instances[i].DomainBase = instance.DomainBase
					state.Instances[i].Status = "created"
					state.Instances[i].Error = ""
				}
			}
			state.UpdatedAt = time.Now().UTC()
			saveErr := m.saveLabState(state)
			stateMu.Unlock()
			if saveErr != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = saveErr
				}
				errMu.Unlock()
			}
		}()
	}
	wait.Wait()
	if firstErr != nil {
		return m.failLab(state, firstErr)
	}
	for _, instanceSpec := range manifest.Spec.Instances {
		if !containsString(instanceSpec.Snapshots, "fresh") {
			continue
		}
		instanceState := findLabInstance(state, instanceSpec.Name)
		if instanceState == nil {
			return m.failLab(state, fmt.Errorf("state is missing instance %q", instanceSpec.Name))
		}
		if _, _, err := m.RunRemote(
			ctx,
			instanceState.InstanceName,
			"set -e; cloud-init status --wait >/dev/null || true; docker info >/dev/null",
		); err != nil {
			return m.failLab(state, fmt.Errorf("base readiness check failed for %s", instanceState.InstanceName))
		}
		if err := m.ensureSnapshot(ctx, instanceState.InstanceName, "fresh", "VPSBox lab fresh baseline"); err != nil {
			return m.failLab(state, err)
		}
	}

	if err := m.configureLabSwarms(ctx, manifest, state, report); err != nil {
		return m.failLab(state, err)
	}

	for _, instanceSpec := range manifest.Spec.Instances {
		instanceState := findLabInstance(state, instanceSpec.Name)
		if instanceState == nil {
			return m.failLab(state, fmt.Errorf("state is missing instance %q", instanceSpec.Name))
		}
		report(fmt.Sprintf("Provisioning %s with %s", instanceState.InstanceName, instanceSpec.Fixture))
		script, err := scenario.ProvisionScript(instanceSpec.Fixture, instanceState.Host)
		if err != nil {
			return m.failLab(state, err)
		}
		if _, _, err := m.RunRemote(ctx, instanceState.InstanceName, encodedRemoteScript(script)); err != nil {
			return m.failLab(state, fmt.Errorf("fixture provisioning failed for %s", instanceState.InstanceName))
		}

		for _, snapshot := range instanceSpec.Snapshots {
			if snapshot == "fresh" {
				continue
			}
			if err := m.ensureSnapshot(ctx, instanceState.InstanceName, snapshot, "VPSBox lab fixture checkpoint"); err != nil {
				return m.failLab(state, err)
			}
		}
		instanceState.Status = "ready"
		instanceState.Error = ""
		state.UpdatedAt = time.Now().UTC()
		if err := m.saveLabState(state); err != nil {
			return m.failLab(state, err)
		}
	}

	state.Status = scenario.LabReady
	state.Error = ""
	state.UpdatedAt = time.Now().UTC()
	if err := m.saveLabState(state); err != nil {
		return nil, err
	}
	report(fmt.Sprintf("Lab %s is ready", runID))
	return state, nil
}

func (m *Manager) LabStatus(runID string) (*scenario.LabState, error) {
	return m.loadLabState(runID)
}

func (m *Manager) SnapshotLab(ctx context.Context, runID, snapshot string) (*scenario.LabState, error) {
	if err := scenario.ValidateRunID(snapshot); err != nil {
		return nil, fmt.Errorf("invalid snapshot name: %w", err)
	}
	state, err := m.loadLabState(runID)
	if err != nil {
		return nil, err
	}
	for _, instance := range state.Instances {
		if err := m.ensureSnapshot(ctx, instance.InstanceName, snapshot, "VPSBox lab checkpoint"); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func (m *Manager) ResetLab(ctx context.Context, runID, snapshot string) (*scenario.LabState, error) {
	if err := scenario.ValidateRunID(snapshot); err != nil {
		return nil, fmt.Errorf("invalid snapshot name: %w", err)
	}
	state, err := m.loadLabState(runID)
	if err != nil {
		return nil, err
	}
	for i := range state.Instances {
		if _, err := m.Reset(ctx, state.Instances[i].InstanceName, snapshot); err != nil {
			return nil, err
		}
		state.Instances[i].Status = "ready"
		state.Instances[i].Error = ""
	}
	state.Status = scenario.LabReady
	state.Error = ""
	state.UpdatedAt = time.Now().UTC()
	if err := m.saveLabState(state); err != nil {
		return nil, err
	}
	return state, nil
}

func (m *Manager) ExportLab(ctx context.Context, runID string) (string, error) {
	state, err := m.loadLabState(runID)
	if err != nil {
		return "", err
	}
	instances := make([]map[string]any, 0, len(state.Instances))
	for _, labInstance := range state.Instances {
		exported, err := m.Export(ctx, labInstance.InstanceName, "sc")
		if err != nil {
			return "", err
		}
		var contract map[string]any
		if err := json.Unmarshal([]byte(exported), &contract); err != nil {
			return "", err
		}
		instances = append(instances, contract)
	}
	sort.Slice(instances, func(i, j int) bool {
		return fmt.Sprint(instances[i]["name"]) < fmt.Sprint(instances[j]["name"])
	})
	result, err := json.MarshalIndent(map[string]any{
		"version":   1,
		"run_id":    runID,
		"status":    state.Status,
		"instances": instances,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func (m *Manager) DestroyLab(ctx context.Context, runID string, force bool) error {
	if !force {
		return errors.New("lab destroy requires --force and an exact run ID")
	}
	state, err := m.loadLabState(runID)
	if err != nil {
		return err
	}
	var destroyErrors []error
	for _, instance := range state.Instances {
		if instance.InstanceName == "" {
			continue
		}
		registered, getErr := m.store.GetInstance(instance.InstanceName)
		if getErr != nil {
			destroyErrors = append(destroyErrors, fmt.Errorf("refusing to destroy %s: registry ownership is unavailable", instance.InstanceName))
			continue
		}
		if registered.ScenarioID != runID {
			destroyErrors = append(destroyErrors, fmt.Errorf("refusing to destroy %s: scenario ownership mismatch", instance.InstanceName))
			continue
		}
		if err := m.Destroy(ctx, instance.InstanceName, true); err != nil && !errors.Is(err, os.ErrNotExist) {
			destroyErrors = append(destroyErrors, err)
		}
	}
	if len(destroyErrors) > 0 {
		return errors.Join(destroyErrors...)
	}
	if err := os.Remove(m.paths.LabStatePath(runID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (m *Manager) configureLabSwarms(
	ctx context.Context,
	manifest scenario.Manifest,
	state *scenario.LabState,
	progress LabProgress,
) error {
	for _, instanceSpec := range manifest.Spec.Instances {
		if instanceSpec.JoinSwarmOf == "" {
			continue
		}
		worker := findLabInstance(state, instanceSpec.Name)
		manager := findLabInstance(state, instanceSpec.JoinSwarmOf)
		if worker == nil || manager == nil {
			return errors.New("Swarm topology references missing lab state")
		}
		if progress != nil {
			progress(fmt.Sprintf("Joining %s to Swarm manager %s", worker.InstanceName, manager.InstanceName))
		}
		initScript := fmt.Sprintf(
			"set -e; cloud-init status --wait >/dev/null || true; docker info >/dev/null; state=$(docker info --format '{{.Swarm.LocalNodeState}}'); if [ \"$state\" = inactive ]; then docker swarm init --advertise-addr %s >/dev/null; fi; docker swarm join-token -q worker",
			remoteShellLiteral(manager.Host),
		)
		token, _, err := m.RunRemote(ctx, manager.InstanceName, initScript)
		if err != nil || strings.TrimSpace(token) == "" {
			return fmt.Errorf("could not obtain an ephemeral Swarm join token for %s", manager.InstanceName)
		}
		joinScript := fmt.Sprintf(
			"set -e; cloud-init status --wait >/dev/null || true; docker info >/dev/null; state=$(docker info --format '{{.Swarm.LocalNodeState}}'); if [ \"$state\" = inactive ]; then docker swarm join --token %s %s:2377 >/dev/null; fi",
			remoteShellLiteral(strings.TrimSpace(token)),
			remoteShellLiteral(manager.Host),
		)
		if _, _, err := m.RunRemote(ctx, worker.InstanceName, joinScript); err != nil {
			return fmt.Errorf("could not join %s to the lab Swarm", worker.InstanceName)
		}
	}
	return nil
}

func (m *Manager) ensureSnapshot(ctx context.Context, instanceName, snapshot, comment string) error {
	existing, err := m.ListSnapshots(ctx, instanceName)
	if err == nil {
		for _, item := range existing {
			if item.Name == snapshot {
				return nil
			}
		}
	}
	return m.Snapshot(ctx, instanceName, snapshot, comment)
}

func (m *Manager) loadOrCreateLabState(
	runID string,
	manifest scenario.Manifest,
	manifestPath string,
) (*scenario.LabState, error) {
	state, err := m.loadLabState(runID)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	now := time.Now().UTC()
	state = &scenario.LabState{
		Version:      labStateVersion,
		RunID:        runID,
		ManifestName: manifest.Metadata.Name,
		ManifestPath: manifestPath,
		Status:       scenario.LabApplying,
		CreatedAt:    now,
		UpdatedAt:    now,
		Instances:    make([]scenario.InstanceState, 0, len(manifest.Spec.Instances)),
	}
	for _, instance := range manifest.Spec.Instances {
		name, err := scenario.InstanceName(runID, instance.Name)
		if err != nil {
			return nil, err
		}
		state.Instances = append(state.Instances, scenario.InstanceState{
			LogicalName:  instance.Name,
			InstanceName: name,
			Role:         instance.Role,
			Fixture:      instance.Fixture,
			Status:       "pending",
		})
	}
	if err := m.saveLabState(state); err != nil {
		return nil, err
	}
	return state, nil
}

func (m *Manager) loadLabState(runID string) (*scenario.LabState, error) {
	if err := scenario.ValidateRunID(runID); err != nil {
		return nil, err
	}
	path := m.paths.LabStatePath(runID)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 1024*1024 {
		return nil, errors.New("lab state must be a regular file smaller than 1 MB")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state scenario.LabState
	if err := json.Unmarshal(content, &state); err != nil {
		return nil, fmt.Errorf("decode lab state: %w", err)
	}
	if state.Version != labStateVersion || state.RunID != runID {
		return nil, errors.New("lab state version or ownership is invalid")
	}
	return &state, nil
}

func (m *Manager) saveLabState(state *scenario.LabState) error {
	if state == nil || state.RunID == "" {
		return errors.New("cannot save empty lab state")
	}
	if err := scenario.ValidateRunID(state.RunID); err != nil {
		return err
	}
	if err := os.MkdirAll(m.paths.LabsDir(), 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(m.paths.LabsDir(), state.RunID+"-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, m.paths.LabStatePath(state.RunID))
}

func (m *Manager) recordLabInstanceError(
	state *scenario.LabState,
	mu *sync.Mutex,
	logicalName string,
	err error,
) {
	mu.Lock()
	defer mu.Unlock()
	if item := findLabInstance(state, logicalName); item != nil {
		item.Status = "failed"
		item.Error = err.Error()
	}
	state.UpdatedAt = time.Now().UTC()
	_ = m.saveLabState(state)
}

func (m *Manager) failLab(state *scenario.LabState, err error) (*scenario.LabState, error) {
	state.Status = scenario.LabFailed
	state.Error = err.Error()
	state.UpdatedAt = time.Now().UTC()
	_ = m.saveLabState(state)
	return state, err
}

func findLabInstance(state *scenario.LabState, logicalName string) *scenario.InstanceState {
	for i := range state.Instances {
		if state.Instances[i].LogicalName == logicalName {
			return &state.Instances[i]
		}
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]bool, len(values)+len(additions))
	result := make([]string, 0, len(values)+len(additions))
	for _, value := range append(values, additions...) {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func encodedRemoteScript(script string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(script))
	return "printf %s " + remoteShellLiteral(encoded) + " | base64 -d | bash"
}

func remoteShellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
