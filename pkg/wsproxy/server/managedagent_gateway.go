// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/controllers/managedagent"
)

// APIKeyHeader is the header every AgentBox API reads its credential from.
const APIKeyHeader = "AGENTBOX-API-KEY"

// WorkspaceFSPrefix routes to the agent's workspace-fs server instead of its
// gateway. The two are separate ports on the Brain, and publishing them as two
// base URLs made every caller carry two addresses and get one of them wrong —
// redirecting only the gateway leaves the file panel and attachment upload
// silently broken. One base URL with a reserved segment keeps them together.
//
// The segment leads with an underscore because everything else under an agent
// is the gateway's own path space: no gateway route starts with one, so this
// cannot shadow a real endpoint now or after the gateway grows new ones.
const WorkspaceFSPrefix = "/_fs"

// brainUserHeader pins which of the agent's end users a request speaks for,
// overriding whatever the request body or query says.
//
// Only a hop that has authenticated the person may set it, and that hop must strip
// any inbound copy — see ManagedAgentAPI.proxy, which is the one place that does.
// The public listener deliberately does NOT set it: an API-key caller is a tenant
// acting for many of its own users, and pinning there would collapse them into one.
const brainUserHeader = "X-Agentbox-User"

// ManagedAgentGateway publishes agents outside the cluster.
//
// It exists because the Brain has no authentication of its own: it takes the
// caller's word for which end user is asking, which is safe only while nothing
// outside the cluster can reach it. This gateway is the hop that makes an
// external route safe — it authenticates the API key, checks that the key's
// owner may use the agent named in the path, and only then forwards.
//
// It deliberately does not terminate the protocol. Everything past the agent
// name is proxied verbatim, so the agent's surface stays whatever the Brain
// serves and this file never has to learn about threads, runs or AG-UI.
type ManagedAgentGateway struct {
	Client    client.Client
	Namespace string

	proxy *httputil.ReverseProxy

	// targetHost replaces the Brain's derived "<service>.<namespace>:<port>".
	//
	// A test seam, and the only one: what this file does that is worth asserting —
	// which headers reach the Brain, and which do not — cannot be observed without
	// a Brain to reach. Empty in production, where the host always comes from the
	// agent, so an unset value cannot silently redirect anyone's traffic.
	targetHost string
}

// NewManagedAgentGateway wires the reverse proxy.
func NewManagedAgentGateway(c client.Client, namespace string) *ManagedAgentGateway {
	g := &ManagedAgentGateway{Client: c, Namespace: namespace}
	g.proxy = &httputil.ReverseProxy{
		Director: func(*http.Request) {}, // set per request in forward()
		// The agent's main response is an SSE event stream. Without immediate
		// flushing the proxy buffers it and the caller sees nothing until the
		// turn ends — which for an agent turn can be minutes, and looks exactly
		// like a hang.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = fmt.Fprintf(w, `{"error":"agent is unreachable","detail":%q}`, err.Error())
		},
	}
	return g
}

// RegisterManagedAgentGatewayRoutes mounts the public surface.
//
// The caller must already be authenticated by the group's middleware — the
// same one the internal API uses, so an AGENTBOX-API-KEY works here exactly as
// it does against a worker cluster. The manager token deliberately does NOT:
// it is the inter-component secret and must not be a credential anyone outside
// the cluster can present.
//
// The route carries the agent name because one route serves every agent: the
// ingress strips the shared public prefix and leaves "/<agent>/<path>".
func (g *ManagedAgentGateway) RegisterManagedAgentGatewayRoutes(r gin.IRouter) {
	r.Any("/:name", g.handle)
	r.Any("/:name/*path", g.handle)
}

func (g *ManagedAgentGateway) handle(c *gin.Context) {
	who := callerOf(c)
	name := c.Param("name")
	var ma agentsv1alpha1.ManagedAgent
	err := g.Client.Get(c.Request.Context(),
		client.ObjectKey{Name: name, Namespace: g.Namespace}, &ma)
	// An agent the caller may not use is reported exactly like one that does
	// not exist. Distinguishing them would let anyone enumerate other tenants'
	// agents by name.
	if err != nil || !visibleTo(&ma, who) {
		c.JSON(http.StatusNotFound, gin.H{"error": "managed agent not found"})
		return
	}
	if ma.Spec.Ingress == nil || !ma.Spec.Ingress.Enabled {
		// Reachable through the ingress but not published: the route is shared
		// by every agent, so this is the per-agent switch.
		c.JSON(http.StatusNotFound, gin.H{"error": "managed agent is not published"})
		return
	}

	g.forward(c, &ma)
}

func (g *ManagedAgentGateway) forward(c *gin.Context, ma *agentsv1alpha1.ManagedAgent) {
	path := c.Param("path")
	if path == "" {
		path = "/"
	}

	port := managedagent.GatewayPort(ma)
	if rest, ok := strings.CutPrefix(path, WorkspaceFSPrefix); ok {
		if rest == "" || strings.HasPrefix(rest, "/") {
			port = managedagent.WorkspaceFSPort(ma)
			path = rest
			if path == "" {
				path = "/"
			}
		}
	}

	host := g.targetHost
	if host == "" {
		host = fmt.Sprintf("%s.%s:%d",
			managedagent.BrainName(ma.Name), ma.Namespace, port)
	}
	target := &url.URL{Scheme: "http", Host: host}

	req := c.Request.Clone(c.Request.Context())
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.URL.Path = path
	req.Host = target.Host
	// The credential authenticates the caller to this gateway and stops here.
	// The Brain has no use for it, and forwarding it would put a platform's key
	// in the logs of a component that never checks it.
	req.Header.Del(APIKeyHeader)
	req.Header.Set("X-Forwarded-Host", c.Request.Host)

	g.proxy.ServeHTTP(c.Writer, req)
}

// NewManagedAgentGatewayServer serves the published agents on their own
// listener, behind auth.
//
// A separate port from the internal API is what keeps the ingress honest: the
// internal API trusts a manager token and must never be routable from outside,
// so the two cannot share a listener that an ingress is pointed at.
func NewManagedAgentGatewayServer(addr string, g *ManagedAgentGateway, auth gin.HandlerFunc) *http.Server {
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	g.RegisterManagedAgentGatewayRoutes(r.Group("/", auth))
	return &http.Server{
		Addr:    addr,
		Handler: r,
		// An agent turn streams for as long as it runs, so the write side must
		// not be bounded here; the read side still is, to bound a stalled
		// client.
		ReadHeaderTimeout: 30 * time.Second,
	}
}
