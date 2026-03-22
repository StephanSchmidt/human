package figma

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/apiclient"
)

// Client is a Figma API client.
type Client struct {
	api *apiclient.Client
}

// New creates a Figma client with the given base URL and personal access token.
func New(baseURL, token string) *Client {
	return &Client{
		api: apiclient.New(baseURL,
			apiclient.WithAuth(apiclient.HeaderAuth("X-Figma-Token", token)),
			apiclient.WithURLBuilder(apiclient.ParsePathURL()),
			apiclient.WithContentType("application/json"),
			apiclient.WithProviderName("figma"),
		),
	}
}

// SetHTTPDoer replaces the HTTP client used for API requests.
func (c *Client) SetHTTPDoer(doer apiclient.HTTPDoer) {
	c.api.SetHTTPDoer(doer)
}

// GetFile fetches file metadata and page listing.
func (c *Client) GetFile(ctx context.Context, fileKey string) (*FileSummary, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/v1/files/"+fileKey+"?depth=1", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var f figmaFile
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding file response", "fileKey", fileKey)
	}

	var pages []PageSummary
	for _, child := range f.Document.Children {
		pages = append(pages, PageSummary{
			ID:         child.ID,
			Name:       child.Name,
			ChildCount: len(child.Children),
		})
	}

	return &FileSummary{
		Name:           f.Name,
		LastModified:   f.LastModified,
		ThumbnailURL:   f.ThumbnailURL,
		Version:        f.Version,
		Pages:          pages,
		ComponentCount: len(f.Components),
	}, nil
}

// GetNodes fetches specific nodes and returns summaries.
func (c *Client) GetNodes(ctx context.Context, fileKey string, nodeIDs []string) ([]NodeSummary, error) {
	ids := encodeNodeIDs(nodeIDs)
	path := "/v1/files/" + fileKey + "/nodes?ids=" + ids

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var nodesResp figmaNodesResponse
	if err := json.NewDecoder(resp.Body).Decode(&nodesResp); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding nodes response", "fileKey", fileKey)
	}

	var summaries []NodeSummary
	for _, id := range nodeIDs {
		if entry, ok := nodesResp.Nodes[id]; ok {
			summaries = append(summaries, SummarizeNode(entry.Document, defaultMaxDepth))
		}
	}
	return summaries, nil
}

// GetFileComponents lists published components in a file.
func (c *Client) GetFileComponents(ctx context.Context, fileKey string) ([]Component, error) {
	path := "/v1/files/" + fileKey + "/components"

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var compResp figmaFileComponentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&compResp); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding components response", "fileKey", fileKey)
	}

	var components []Component
	for _, c := range compResp.Meta.Components {
		components = append(components, Component{
			Key:         c.Key,
			NodeID:      c.NodeID,
			Name:        c.Name,
			Description: c.Description,
			Page:        c.ContainingFrame.PageName,
			Frame:       c.ContainingFrame.Name,
		})
	}
	return components, nil
}

// GetFileComments lists comments on a file.
func (c *Client) GetFileComments(ctx context.Context, fileKey string) ([]FileComment, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/v1/files/"+fileKey+"/comments", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var commResp figmaCommentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&commResp); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding comments response", "fileKey", fileKey)
	}

	var comments []FileComment
	for _, c := range commResp.Comments {
		nodeID := ""
		if c.ClientMeta != nil {
			nodeID = c.ClientMeta.NodeID
		}
		comments = append(comments, FileComment{
			ID:        c.ID,
			Author:    c.User.Handle,
			Message:   c.Message,
			CreatedAt: c.CreatedAt,
			Resolved:  c.ResolvedAt != nil,
			NodeID:    nodeID,
			ParentID:  c.ParentID,
		})
	}
	return comments, nil
}

// ExportImages exports nodes as images and returns temporary URLs.
func (c *Client) ExportImages(ctx context.Context, fileKey string, nodeIDs []string, format string) ([]ImageExport, error) {
	if format == "" {
		format = "png"
	}
	ids := encodeNodeIDs(nodeIDs)
	path := "/v1/images/" + fileKey + "?ids=" + ids + "&format=" + format

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var imgResp figmaImagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&imgResp); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding images response", "fileKey", fileKey)
	}

	var exports []ImageExport
	for _, id := range nodeIDs {
		if u, ok := imgResp.Images[id]; ok {
			exports = append(exports, ImageExport{NodeID: id, URL: u})
		}
	}
	return exports, nil
}

// ListProjects lists projects for a team.
func (c *Client) ListProjects(ctx context.Context, teamID string) ([]Project, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/v1/teams/"+teamID+"/projects", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var projResp figmaProjectsResponse
	if err := json.NewDecoder(resp.Body).Decode(&projResp); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding projects response", "teamID", teamID)
	}

	var projects []Project
	for _, p := range projResp.Projects {
		projects = append(projects, Project(p))
	}
	return projects, nil
}

// ListProjectFiles lists files in a project.
func (c *Client) ListProjectFiles(ctx context.Context, projectID string) ([]ProjectFile, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/v1/projects/"+projectID+"/files", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var filesResp figmaProjectFilesResponse
	if err := json.NewDecoder(resp.Body).Decode(&filesResp); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding project files response", "projectID", projectID)
	}

	var files []ProjectFile
	for _, f := range filesResp.Files {
		files = append(files, ProjectFile(f))
	}
	return files, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	return c.api.Do(ctx, method, path, "", body)
}

// encodeNodeIDs joins and URL-encodes node IDs for query parameters.
func encodeNodeIDs(ids []string) string {
	encoded := make([]string, len(ids))
	for i, id := range ids {
		encoded[i] = url.QueryEscape(id)
	}
	return strings.Join(encoded, ",")
}
