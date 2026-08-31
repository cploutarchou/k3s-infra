package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// The write path: no kubectl apply, ever. Changes are proposed as a GitHub
// PR against this repo and land on the cluster via Flux after review.

const githubAPI = "https://api.github.com"

type ghClient struct {
	token string
	owner string
	repo  string
	http  *http.Client
}

func newGHClient() (*ghClient, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN not configured on the server")
	}
	owner := os.Getenv("GITHUB_OWNER")
	repo := os.Getenv("GITHUB_REPO")
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("GITHUB_OWNER / GITHUB_REPO not configured")
	}
	return &ghClient{token: token, owner: owner, repo: repo,
		http: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (g *ghClient) call(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, githubAPI+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("github %s %s: %s: %s", method, path, resp.Status, string(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func registerGitHub(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("propose_change",
			mcp.WithDescription("Open a GitHub PR against the infra repo with one file added or updated on a new branch. This is the only way to change cluster state: the PR is reviewed, merged, and Flux reconciles it."),
			mcp.WithString("branch", mcp.Required(), mcp.Description("New branch name, e.g. mcp/update-traefik.")),
			mcp.WithString("path", mcp.Required(), mcp.Description("Repo-relative file path, e.g. clusters/prod/apps/foo/deployment.yaml.")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Full new file content (UTF-8).")),
			mcp.WithString("title", mcp.Required(), mcp.Description("PR title (conventional commit style).")),
			mcp.WithString("body", mcp.Description("PR description: what changes and why, expected reconcile impact.")),
			mcp.WithString("base", mcp.Description("Base branch (default master).")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			gh, err := newGHClient()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			branch, err := req.RequireString("branch")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			path, err := req.RequireString("path")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			content, err := req.RequireString("content")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			title, err := req.RequireString("title")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			base := req.GetString("base", "master")
			repoPath := fmt.Sprintf("/repos/%s/%s", gh.owner, gh.repo)

			// Base ref SHA -> new branch.
			var ref struct {
				Object struct {
					SHA string `json:"sha"`
				} `json:"object"`
			}
			if err := gh.call(ctx, http.MethodGet, repoPath+"/git/ref/heads/"+base, nil, &ref); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := gh.call(ctx, http.MethodPost, repoPath+"/git/refs", map[string]string{
				"ref": "refs/heads/" + branch,
				"sha": ref.Object.SHA,
			}, nil); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			// Create or update the file on the branch (needs the blob SHA if
			// the file already exists).
			put := map[string]any{
				"message": title,
				"content": base64.StdEncoding.EncodeToString([]byte(content)),
				"branch":  branch,
			}
			var existing struct {
				SHA string `json:"sha"`
			}
			if err := gh.call(ctx, http.MethodGet,
				fmt.Sprintf("%s/contents/%s?ref=%s", repoPath, path, base), nil, &existing); err == nil && existing.SHA != "" {
				put["sha"] = existing.SHA
			}
			if err := gh.call(ctx, http.MethodPut, repoPath+"/contents/"+path, put, nil); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			var pr struct {
				Number  int    `json:"number"`
				HTMLURL string `json:"html_url"`
			}
			if err := gh.call(ctx, http.MethodPost, repoPath+"/pulls", map[string]string{
				"title": title,
				"head":  branch,
				"base":  base,
				"body":  req.GetString("body", ""),
			}, &pr); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("PR #%d opened: %s", pr.Number, pr.HTMLURL)), nil
		},
	)
}
