package stagegraph

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/atm1020/gojenkins"
)

const (
	overviewPathSegment = "/pipeline-overview"
	legacyPathSegment   = "/pipeline-graph"
	treeEndpointSuffix  = "tree"
	maxGraphBodyBytes   = 4 << 20
)

// FetchTree fetches the optional pipeline-graph-view stage tree for run using
// the server's cached capability state.
//
// Probing happens lazily on the first request for a server:
//   - Try <buildBase>/pipeline-overview/tree (modern endpoint).
//   - On HTTP 404, try <buildBase>/pipeline-graph/tree (legacy endpoint).
//   - Only HTTP 404 on both endpoints caches CapabilityUnavailable.
//   - Transient errors (network failures, 5xx, invalid bodies, context
//     cancellation) return (nil, false, err) without poisoning the cache.
func FetchTree(
	ctx context.Context,
	jenkins *gojenkins.Jenkins,
	cache *CapabilityCache,
	serverKey string,
	run gojenkins.PipelineRun,
) (*PluginTree, bool, error) {
	if jenkins == nil || jenkins.Requester == nil {
		return nil, false, nil
	}
	buildBase := normalizeBuildBase(jenkins.Server, run)
	if buildBase == "" {
		return nil, false, nil
	}
	if serverKey == "" {
		serverKey = jenkins.Server
	}

	switch cache.Get(serverKey) {
	case CapabilityUnavailable:
		return nil, false, nil
	case CapabilityOverview:
		tree, status, err := fetchEndpoint(ctx, jenkins, buildBase+overviewPathSegment)
		if err == nil && status == http.StatusOK {
			return tree, true, nil
		}
		if status == http.StatusNotFound {
			return probeLegacy(ctx, jenkins, cache, serverKey, buildBase)
		}
		return nil, false, err
	case CapabilityLegacy:
		tree, status, err := fetchEndpoint(ctx, jenkins, buildBase+legacyPathSegment)
		if err == nil && status == http.StatusOK {
			return tree, true, nil
		}
		if status == http.StatusNotFound {
			cache.Set(serverKey, CapabilityUnavailable)
			return nil, false, nil
		}
		return nil, false, err
	default:
		tree, status, err := fetchEndpoint(ctx, jenkins, buildBase+overviewPathSegment)
		if err == nil && status == http.StatusOK {
			cache.Set(serverKey, CapabilityOverview)
			return tree, true, nil
		}
		if status == http.StatusNotFound {
			return probeLegacy(ctx, jenkins, cache, serverKey, buildBase)
		}
		return nil, false, err
	}
}

func probeLegacy(
	ctx context.Context,
	jenkins *gojenkins.Jenkins,
	cache *CapabilityCache,
	serverKey, buildBase string,
) (*PluginTree, bool, error) {
	tree, status, err := fetchEndpoint(ctx, jenkins, buildBase+legacyPathSegment)
	if err == nil && status == http.StatusOK {
		cache.Set(serverKey, CapabilityLegacy)
		return tree, true, nil
	}
	if status == http.StatusNotFound {
		cache.Set(serverKey, CapabilityUnavailable)
		return nil, false, nil
	}
	return nil, false, err
}

func fetchEndpoint(ctx context.Context, jenkins *gojenkins.Jenkins, baseSegment string) (*PluginTree, int, error) {
	ar := gojenkins.NewAPIRequest(http.MethodGet, baseSegment, nil)
	ar.SetHeader("Accept", "application/json")
	ar.Suffix = treeEndpointSuffix

	resp, err := jenkins.Requester.Do(ctx, ar, nil)
	if err != nil {
		return nil, 0, err
	}
	if resp == nil {
		return nil, 0, fmt.Errorf("empty response for %s/%s", baseSegment, treeEndpointSuffix)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil, http.StatusNotFound, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("pipeline graph endpoint %s/%s returned HTTP %d", baseSegment, treeEndpointSuffix, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGraphBodyBytes))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	tree, err := DecodePluginTree(body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return tree, http.StatusOK, nil
}

func normalizeBuildBase(serverURL string, run gojenkins.PipelineRun) string {
	base := strings.TrimSpace(run.Base)
	if base == "" && run.Job != nil && run.Job.Base != "" && run.ID != "" {
		base = strings.TrimRight(run.Job.Base, "/") + "/" + run.ID
	}
	if base == "" {
		return ""
	}
	server := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if server != "" && strings.HasPrefix(base, server) {
		base = strings.TrimPrefix(base, server)
	}
	base = strings.TrimRight(base, "/")
	if base == "" {
		return ""
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	return base
}
