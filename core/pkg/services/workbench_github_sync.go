package services

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func mergeGitHubIncremental(before, observed []common.WorkbenchTask) []common.WorkbenchTask {
	merged := make([]common.WorkbenchTask, len(before))
	copy(merged, before)
	indices := make(map[string]int, len(before))
	for i, task := range before {
		key, _, _ := strings.Cut(task.SourceKey, "\x00")
		indices[key] = i
		merged[i].SourceKey = key
	}
	for _, candidate := range observed {
		index, exists := indices[candidate.SourceKey]
		if !exists {
			indices[candidate.SourceKey] = len(merged)
			merged = append(merged, candidate)
			continue
		}
		priorTime, priorErr := time.Parse(time.RFC3339Nano, merged[index].NativeFields["updated_at"])
		nextTime, nextErr := time.Parse(time.RFC3339Nano, candidate.NativeFields["updated_at"])
		if priorErr == nil && (nextErr != nil || nextTime.Before(priorTime)) {
			continue
		}
		merged[index] = candidate
	}
	return merged
}

type workbenchGitHubSyncStore interface {
	WorkbenchGitHubProgress(string) (common.WorkbenchGitHubProgress, error)
	CommitWorkbenchGitHubSync(common.WorkbenchConnection, []common.WorkbenchAttempt, []common.WorkbenchTask, []common.WorkbenchTask, time.Time, int64, time.Time, bool) error
}

func (s *Service) readWorkbenchGitHub(ctx context.Context, c common.WorkbenchConnection, before []common.WorkbenchTask) ([]common.WorkbenchTask, time.Time, bool, error) {
	store, ok := s.store.(workbenchGitHubSyncStore)
	if !ok {
		return nil, time.Time{}, false, common.Err("INTERNAL", "GitHub synchronization storage unavailable")
	}
	progress, err := store.WorkbenchGitHubProgress(c.ID)
	if err != nil {
		return nil, time.Time{}, false, err
	}
	credentials, err := s.workbenchGitHubCredentials()
	if err != nil {
		return nil, time.Time{}, false, err
	}
	token, release, err := s.acquireGitHubCredential(ctx, credentials, progress.CredentialRef)
	if err != nil {
		return nil, time.Time{}, false, err
	}
	defer release()
	client, err := s.workbenchGitHubClient(token)
	if err != nil {
		return nil, time.Time{}, false, err
	}
	client.CoordinateRequests(s.githubRate, progress.AccountID)
	started := time.Now().UTC()
	if s.githubNow != nil {
		started = s.githubNow().UTC()
	}
	repository, err := client.Repository(ctx, c.Repository)
	if err != nil {
		return nil, started, false, err
	}
	if repository.ID != progress.RepositoryID || !strings.EqualFold(repository.FullName, c.Repository) || !repository.HasIssues {
		return nil, started, false, common.Err("CONFLICT", "GitHub repository identity or name changed")
	}
	credentialValue, err := credentials.Get(ctx, progress.CredentialRef)
	if err != nil {
		return nil, started, false, err
	}
	if err := s.verifyGitHubCredentialInstallation(ctx, client, credentialValue, repository.ID); err != nil {
		return nil, started, false, err
	}
	full := progress.Cursor == nil || progress.LastFullSync == nil || started.Sub(*progress.LastFullSync) >= 24*time.Hour || progress.AccessState != "available"
	var since *time.Time
	if !full {
		window := progress.Cursor.Add(-5 * time.Minute)
		since = &window
	}
	tasks, err := client.Issues(ctx, c, since, progress.RepositoryID)
	if err != nil || !full {
		return tasks, started, full, err
	}
	seen := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		seen[task.SourceKey] = true
	}
	for _, previous := range before {
		key, _, _ := strings.Cut(previous.SourceKey, "\x00")
		previous.SourceKey = key
		if seen[key] {
			continue
		}
		number, err := strconv.ParseInt(previous.NativeID, 10, 64)
		if err != nil {
			if progress.AccessState == "available" {
				tasks = append(tasks, previous)
			}
			continue
		}
		current, err := client.Issue(ctx, c, number)
		if err != nil {
			var problem *common.Error
			if errors.As(err, &problem) && (problem.Code == "ISSUE_MOVED" || problem.Code == "ISSUE_DELETED") {
				repo, repoErr := client.Repository(ctx, c.Repository)
				if repoErr != nil {
					return nil, started, full, repoErr
				}
				if repo.ID != progress.RepositoryID || !strings.EqualFold(repo.FullName, c.Repository) || !repo.HasIssues {
					return nil, started, full, common.Err("CONFLICT", "GitHub repository identity or name changed")
				}
				continue
			}
			if errors.As(err, &problem) && problem.Code == "NOT_FOUND" {
				// A private repository can return 404 for missing permissions.
				// Keep the previous object until absence can be established.
				if progress.AccessState == "available" {
					tasks = append(tasks, previous)
				}
				continue
			}
			return nil, started, full, err
		}
		if current.SourceKey == key {
			tasks = append(tasks, current)
		}
	}
	return tasks, started, full, nil
}
