package web

import (
	"errors"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

func (a *App) chatSessionForProject(project store.Project) (store.ChatSession, error) {
	return a.store.GetOrCreateChatSession(project.ProjectKey)
}

func (a *App) projectForWorkspace(workspace store.Workspace) (*store.Project, error) {
	if workspace.ProjectID == nil || strings.TrimSpace(*workspace.ProjectID) == "" {
		return nil, nil
	}
	project, err := a.store.GetProject(strings.TrimSpace(*workspace.ProjectID))
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (a *App) workspaceForChatSession(session store.ChatSession) (store.Workspace, error) {
	if session.WorkspaceID <= 0 {
		return store.Workspace{}, errors.New("chat session workspace is required")
	}
	workspace, err := a.store.GetWorkspace(session.WorkspaceID)
	if err != nil {
		return store.Workspace{}, err
	}
	if strings.TrimSpace(workspace.DirPath) == "" {
		return store.Workspace{}, errors.New("workspace path is required")
	}
	return workspace, nil
}

func (a *App) workspaceDirForChatSession(session store.ChatSession) (string, error) {
	workspace, err := a.workspaceForChatSession(session)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(workspace.DirPath), nil
}

func (a *App) workspaceDirForChatSessionID(sessionID string) (string, error) {
	session, err := a.store.GetChatSession(sessionID)
	if err != nil {
		return "", err
	}
	return a.workspaceDirForChatSession(session)
}

func (a *App) resolveChatSessionTarget(projectID, projectKey string, workspaceID *int64) (store.Workspace, *store.Project, error) {
	if workspaceID != nil {
		workspace, err := a.store.GetWorkspace(*workspaceID)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		project, err := a.projectForWorkspace(workspace)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		return workspace, project, nil
	}

	loadProject := func(project store.Project) (store.Workspace, *store.Project, error) {
		session, err := a.chatSessionForProject(project)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		workspace, err := a.store.GetWorkspace(session.WorkspaceID)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		projectForWorkspace, err := a.projectForWorkspace(workspace)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		return workspace, projectForWorkspace, nil
	}

	if id := strings.TrimSpace(projectID); id != "" {
		project, err := a.store.GetProject(id)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		return loadProject(project)
	}
	if key := strings.TrimSpace(projectKey); key != "" {
		project, err := a.store.GetProjectByProjectKey(key)
		if err == nil {
			return loadProject(project)
		}
		if !isNoRows(err) {
			return store.Workspace{}, nil, err
		}
		workspace, workspaceErr := a.store.GetWorkspaceByPath(key)
		if workspaceErr != nil {
			return store.Workspace{}, nil, workspaceErr
		}
		projectForWorkspace, err := a.projectForWorkspace(workspace)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		return workspace, projectForWorkspace, nil
	}

	if workspace, err := a.store.ActiveWorkspace(); err == nil {
		if strings.TrimSpace(a.localProjectDir) != "" {
			if startupErr := a.ensureStartupProjectWithWorkspace(); startupErr == nil {
				project, projectErr := a.ensureDefaultProjectRecord()
				if projectErr != nil {
					return store.Workspace{}, nil, projectErr
				}
				session, sessionErr := a.chatSessionForProject(project)
				if sessionErr != nil {
					return store.Workspace{}, nil, sessionErr
				}
				workspace, err = a.store.GetWorkspace(session.WorkspaceID)
				if err != nil {
					return store.Workspace{}, nil, err
				}
				return workspace, &project, nil
			}
		}
		if workspace.IsDaily && workspaceDailyDate(workspace) != dailyWorkspaceDate(a.runtimeNow()) {
			workspace, err = a.ensureTodayDailyWorkspace()
			if err != nil {
				return store.Workspace{}, nil, err
			}
		}
		project, err := a.projectForWorkspace(workspace)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		return workspace, project, nil
	} else if !isNoRows(err) {
		return store.Workspace{}, nil, err
	}

	if strings.TrimSpace(a.localProjectDir) != "" {
		if err := a.ensureStartupProjectWithWorkspace(); err != nil {
			return store.Workspace{}, nil, err
		}
		project, err := a.ensureDefaultProjectRecord()
		if err != nil {
			return store.Workspace{}, nil, err
		}
		session, err := a.chatSessionForProject(project)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		workspace, err := a.store.GetWorkspace(session.WorkspaceID)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		return workspace, &project, nil
	}
	workspace, err := a.ensureTodayDailyWorkspace()
	if err != nil {
		return store.Workspace{}, nil, err
	}
	project, err := a.projectForWorkspace(workspace)
	if err != nil {
		return store.Workspace{}, nil, err
	}
	return workspace, project, nil
}

func (a *App) chatSessionForProjectKey(projectKey string) (store.ChatSession, error) {
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return store.ChatSession{}, errors.New("project key is required")
	}
	project, err := a.store.GetProjectByProjectKey(key)
	if err == nil {
		return a.chatSessionForProject(project)
	}
	if !isNoRows(err) {
		return store.ChatSession{}, err
	}
	return a.store.GetChatSessionByProjectKey(key)
}
