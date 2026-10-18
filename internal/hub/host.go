package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// A signature proves what a bird's CI built; it can't prove the link is
// running it, since an altered build could present a statement it copied.
// On her own host cygnet can see further: the Docker daemon knows which
// container a connection on the fleet network comes from and which image
// it runs. A bird whose container runs the very image its attestation
// names is attested; one whose container runs anything else is turned
// away. A link through cygnet's own tunnel is a bird on another host,
// where she can see nothing, and stays signed.

// Host tells cygnet what runs behind a link on her host.
type Host interface {
	// Images is every digest the image of the container at ip answers to,
	// and whether that container is one of cygnet's own (her tunnel).
	Images(ctx context.Context, ip string) (digests []string, own bool, err error)
}

// Watch has h check each signed link against host. Call it before the hub
// serves anyone.
func (h *Hub) Watch(host Host) { h.host = host }

var errNoContainer = errors.New("hub: no container on the fleet network has that address")

// Docker is Host over the Docker Engine API, read through a proxy that
// allows only reading containers and images (docker-compose.prod.yml).
type Docker struct {
	URL     string // the proxy, http://docker:2375
	Client  *http.Client
	Project string // cygnet's own compose project, whose containers are hers
	Network string // the fleet network
}

func (d Docker) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(d.URL, "/")+path, nil)
	if err != nil {
		return err
	}
	res, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("hub: docker answered %s for %s", res.Status, path)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(v)
}

func (d Docker) Images(ctx context.Context, ip string) ([]string, bool, error) {
	var cs []struct {
		ImageID         string
		Labels          map[string]string
		NetworkSettings struct {
			Networks map[string]struct{ IPAddress string }
		}
	}
	if err := d.get(ctx, "/containers/json", &cs); err != nil {
		return nil, false, err
	}
	for _, c := range cs {
		if n, ok := c.NetworkSettings.Networks[d.Network]; !ok || n.IPAddress != ip || ip == "" {
			continue
		}
		own := c.Labels["com.docker.compose.project"] == d.Project
		digests := []string{c.ImageID}
		var img struct{ RepoDigests []string }
		if err := d.get(ctx, "/images/"+url.PathEscape(c.ImageID)+"/json", &img); err != nil {
			return nil, own, err
		}
		for _, r := range img.RepoDigests {
			if _, dg, ok := strings.Cut(r, "@"); ok {
				digests = append(digests, dg)
			}
		}
		return digests, own, nil
	}
	return nil, false, errNoContainer
}
