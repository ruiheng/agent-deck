package session

import (
	"bytes"
	"path/filepath"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/git"
)

type MultiRepoWorktreeResult struct {
	MappedPaths []string
	Worktrees   []MultiRepoWorktree
	Warnings    []string
}

func CreateMultiRepoWorktrees(allPaths []string, parentDir string, branch string, setupTimeout time.Duration) MultiRepoWorktreeResult {
	var result MultiRepoWorktreeResult
	dirnames := DeduplicateDirnames(allPaths)

	for i, p := range allPaths {
		wtPath := filepath.Join(parentDir, dirnames[i])

		if git.IsGitRepoOrBareProjectRoot(p) {
			repoRoot, rootErr := git.GetWorktreeBaseRoot(p)
			if rootErr != nil {
				result.Warnings = append(result.Warnings, "worktree_skip: "+p+": "+rootErr.Error())
				if err := createMultiRepoPathAlias(p, wtPath); err != nil {
					result.Warnings = append(result.Warnings, "path_alias_fail: "+p+": "+err.Error())
				}
				result.MappedPaths = append(result.MappedPaths, wtPath)
				continue
			}

			var buf bytes.Buffer
			setupErr, err := git.CreateWorktreeWithSetup(repoRoot, wtPath, branch, &buf, &buf, setupTimeout)
			if err != nil {
				result.Warnings = append(result.Warnings, "worktree_create_fail: "+p+": "+err.Error())
				if err := createMultiRepoPathAlias(p, wtPath); err != nil {
					result.Warnings = append(result.Warnings, "path_alias_fail: "+p+": "+err.Error())
				}
				result.MappedPaths = append(result.MappedPaths, wtPath)
				continue
			}
			if setupErr != nil {
				result.Warnings = append(result.Warnings, "worktree_setup_fail: "+p+": "+setupErr.Error())
			}

			result.Worktrees = append(result.Worktrees, MultiRepoWorktree{
				OriginalPath: p,
				WorktreePath: wtPath,
				RepoRoot:     repoRoot,
				Branch:       branch,
			})
			result.MappedPaths = append(result.MappedPaths, wtPath)
		} else {
			if err := createMultiRepoPathAlias(p, wtPath); err != nil {
				result.Warnings = append(result.Warnings, "path_alias_fail: "+p+": "+err.Error())
			}
			result.MappedPaths = append(result.MappedPaths, wtPath)
		}
	}

	return result
}

func createMultiRepoPathAlias(source, target string) error {
	return createMultiRepoPathAliasPlatform(source, target)
}
