package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

const GroupNamingSchemaVersion = 1

var groupNamingComponentPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var workItemIDPattern = regexp.MustCompile(`^[a-z0-9]+$`)
var repositoryIdentityComponentPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type GroupNamingProject struct {
	Repository string `json:"repository"`
	Prefix     string `json:"prefix"`
}

type GroupNamingConfig struct {
	SchemaVersion int                  `json:"schema_version"`
	Projects      []GroupNamingProject `json:"projects"`
}

func LoadGroupNaming(path string) (GroupNamingConfig, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return GroupNamingConfig{}, fmt.Errorf("group naming config is missing: %s", path)
	}
	if err != nil {
		return GroupNamingConfig{}, fmt.Errorf("open group naming config: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, 64*1024+1))
	decoder.DisallowUnknownFields()
	var config GroupNamingConfig
	if err := decoder.Decode(&config); err != nil {
		return GroupNamingConfig{}, fmt.Errorf("parse group naming config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return GroupNamingConfig{}, errors.New("parse group naming config: multiple JSON values")
		}
		return GroupNamingConfig{}, fmt.Errorf("parse group naming config: %w", err)
	}
	if config.SchemaVersion != GroupNamingSchemaVersion {
		return GroupNamingConfig{}, fmt.Errorf("unsupported group naming schema version %d", config.SchemaVersion)
	}
	if len(config.Projects) == 0 {
		return GroupNamingConfig{}, errors.New("group naming config must contain at least one project")
	}
	seen := make(map[string]bool, len(config.Projects))
	for index, project := range config.Projects {
		if err := ValidateRepositoryIdentity(project.Repository); err != nil {
			return GroupNamingConfig{}, fmt.Errorf("invalid group naming repository at project %d: %w", index+1, err)
		}
		if seen[project.Repository] {
			return GroupNamingConfig{}, fmt.Errorf("ambiguous group naming config: repository %s is configured more than once", project.Repository)
		}
		if len(project.Prefix) > 32 {
			return GroupNamingConfig{}, fmt.Errorf("project prefix for repository %s exceeds 32 characters", project.Repository)
		}
		if !groupNamingComponentPattern.MatchString(project.Prefix) || strings.Contains(project.Prefix, "-") {
			return GroupNamingConfig{}, fmt.Errorf("invalid project prefix %q for repository %s: must contain only lowercase letters and digits", project.Prefix, project.Repository)
		}
		seen[project.Repository] = true
	}
	sort.Slice(config.Projects, func(i, j int) bool { return config.Projects[i].Repository < config.Projects[j].Repository })
	return config, nil
}

func ValidateRepositoryIdentity(repository string) error {
	if len(repository) > 255 {
		return errors.New("repository identity exceeds 255 characters")
	}
	parts := strings.Split(repository, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return fmt.Errorf("repository identity %q must be lowercase host/owner/repository with non-empty components", repository)
	}
	if repository != strings.ToLower(repository) || strings.ContainsAny(repository, " \t\r\n:@") {
		return fmt.Errorf("repository identity %q must be lowercase host/owner/repository", repository)
	}
	for _, part := range parts {
		if !repositoryIdentityComponentPattern.MatchString(part) || part == "." || part == ".." {
			return fmt.Errorf("repository identity %q contains an invalid host, owner, or repository component", repository)
		}
	}
	return nil
}

func (c GroupNamingConfig) Project(repository string) (GroupNamingProject, error) {
	index := sort.Search(len(c.Projects), func(i int) bool { return c.Projects[i].Repository >= repository })
	if index >= len(c.Projects) || c.Projects[index].Repository != repository {
		return GroupNamingProject{}, fmt.Errorf("group naming config has no project matching verified repository %s", repository)
	}
	return c.Projects[index], nil
}

func DeriveGroupNaming(prefix, workItemID, slug string, ordinal int) (groupID, reportID string, err error) {
	if len(workItemID) > 32 {
		return "", "", errors.New("work-item ID must be at most 32 characters")
	}
	if !workItemIDPattern.MatchString(workItemID) {
		return "", "", fmt.Errorf("invalid work-item ID %q: must contain only lowercase letters and digits", workItemID)
	}
	if len(slug) > 32 {
		return "", "", errors.New("group slug must be at most 32 characters")
	}
	if !groupNamingComponentPattern.MatchString(slug) {
		return "", "", fmt.Errorf("invalid group slug %q: must match ^[a-z0-9]+(?:-[a-z0-9]+)*$ exactly", slug)
	}
	if ordinal < 1 {
		return "", "", errors.New("worker ordinal must be a positive integer")
	}
	groupID = strings.Join([]string{prefix, workItemID, slug}, "-")
	if err := ValidateGroupID(groupID); err != nil {
		return "", "", fmt.Errorf("derived group ID rejected without truncation: %w", err)
	}
	reportID = fmt.Sprintf("%s-worker-%d", groupID, ordinal)
	return groupID, reportID, nil
}
